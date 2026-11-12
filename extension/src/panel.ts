import { randomBytes } from "crypto";
import * as vscode from "vscode";

/** The softgaze server, always on this machine. */
export function baseUrl(): string {
  const port = vscode.workspace.getConfiguration("softgaze").get<number>("port", 3133);
  return `http://127.0.0.1:${port}`;
}

/**
 * The Softgaze page in a webview panel (an iframe on the local server). When
 * the server is not running it says so, with Retry.
 */
export class SoftgazePanel {
  private static current?: SoftgazePanel;
  private static readonly activeChanged = new vscode.EventEmitter<void>();
  /** Fires when the panel becomes or stops being the active editor, or closes. */
  static readonly onDidChangeActive = SoftgazePanel.activeChanged.event;
  readonly panel: vscode.WebviewPanel;
  private disposed = false;

  /** Shows the panel; returns true when it was already open. */
  static show(extensionUri: vscode.Uri): boolean {
    if (SoftgazePanel.current) {
      SoftgazePanel.current.reveal();
      void SoftgazePanel.current.update();
      return true;
    }
    SoftgazePanel.current = new SoftgazePanel(extensionUri);
    return false;
  }

  static refresh(): void {
    void SoftgazePanel.current?.update();
  }

  /** Whether the Softgaze panel is the active editor (its window in front). */
  static isActive(): boolean {
    return SoftgazePanel.current?.panel.active ?? false;
  }

  /**
   * Brings the panel forward where it is, in its floating window, and that
   * window to the front. Its own column keeps it there: without one VS Code
   * would open it in the active group, out of the floating window.
   */
  private reveal(): void {
    this.panel.reveal(this.panel.viewColumn, false);
    // The window holding the active editor, to the front even from another
    // VS Code window (older VS Code has no such command).
    void vscode.commands.executeCommand("workbench.action.focusWindow").then(undefined, () => undefined);
  }

  private constructor(extensionUri: vscode.Uri) {
    this.panel = vscode.window.createWebviewPanel("softgaze", "Softgaze", vscode.ViewColumn.Active, {
      enableScripts: true,
      retainContextWhenHidden: true,
    });
    this.panel.iconPath = vscode.Uri.joinPath(extensionUri, "softgaze.svg");
    this.panel.onDidDispose(() => {
      this.disposed = true;
      SoftgazePanel.current = undefined;
      SoftgazePanel.activeChanged.fire();
    });
    this.panel.onDidChangeViewState(() => SoftgazePanel.activeChanged.fire());
    this.panel.webview.onDidReceiveMessage((msg) => {
      if (msg?.type === "retry") {
        void this.update();
      } else if (msg?.type === "fullscreen") {
        // The page asked (F): full screen for the window it is in.
        void vscode.commands.executeCommand("workbench.action.toggleFullScreen");
      } else if (msg?.type === "reveal") {
        // Ctrl+Alt+J in some VS Code window, relayed by softgaze.
        this.reveal();
      }
    });
    void this.update();
  }

  private async update(): Promise<void> {
    if (this.disposed) {
      return;
    }
    const url = baseUrl();
    this.panel.webview.html = (await ping(url)) ? embedHtml(url) : offlineHtml(url);
  }
}

async function ping(url: string): Promise<boolean> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 3000);
  try {
    return (await fetch(`${url}/api/info`, { signal: ctrl.signal })).ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

function embedHtml(url: string): string {
  const nonce = makeNonce();
  const csp = ["default-src 'none'", `frame-src ${url}`, "style-src 'unsafe-inline'", `script-src 'nonce-${nonce}'`].join("; ");
  const origin = JSON.stringify(new URL(url).origin);
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="${csp}" />
  <style>
    html, body { margin: 0; padding: 0; height: 100%; overflow: hidden; }
    iframe { border: 0; width: 100%; height: 100vh; display: block; }
  </style>
</head>
<body>
  <iframe id="page" src="${url}/follow.html" title="Softgaze" allow="clipboard-write"></iframe>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    const frame = document.getElementById('page');
    const origin = ${origin};
    // Only the Softgaze page, from the local server, may ask for full screen
    // or for the panel to come forward.
    window.addEventListener('message', (e) => {
      if (e.source !== frame.contentWindow || e.origin !== origin || !e.data) return;
      if (e.data.type === 'softgaze:fullscreen') {
        vscode.postMessage({ type: 'fullscreen' });
      } else if (e.data.type === 'softgaze:reveal') {
        vscode.postMessage({ type: 'reveal' });
      }
    });
  </script>
</body>
</html>`;
}

function offlineHtml(url: string): string {
  const nonce = makeNonce();
  return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-${nonce}';" />
  <style>
    body { font-family: var(--vscode-font-family); color: var(--vscode-foreground);
      display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; text-align: center; }
    .box { max-width: 28rem; padding: 2rem; }
    code { background: var(--vscode-textCodeBlock-background); padding: 0.1rem 0.3rem; border-radius: 3px; }
    p { color: var(--vscode-descriptionForeground); }
    button { margin-top: 1rem; padding: 0.5rem 1rem; cursor: pointer; color: var(--vscode-button-foreground);
      background: var(--vscode-button-background); border: none; border-radius: 3px; }
  </style>
</head>
<body>
  <div class="box">
    <h2>softgaze is not running</h2>
    <p>Nothing answers at <code>${url}</code>. Start <code>softgaze</code> (or <code>softgaze service install</code> to have it start on login), then Retry.</p>
    <button id="retry">Retry</button>
  </div>
  <script nonce="${nonce}">
    const vscode = acquireVsCodeApi();
    document.getElementById('retry').addEventListener('click', () => vscode.postMessage({ type: 'retry' }));
  </script>
</body>
</html>`;
}

/** A fresh nonce for the webview's CSP, from the system's secure random source. */
function makeNonce(): string {
  return randomBytes(18).toString("base64");
}
