import * as vscode from "vscode";
import { baseUrl, SoftgazePanel } from "./panel";

/** This VS Code window, for its reports to softgaze. */
const windowId = Math.random().toString(36).slice(2, 14) || "window";

export function activate(context: vscode.ExtensionContext): void {
  const report = () => void reportWindow(false);
  context.subscriptions.push(
    vscode.commands.registerCommand("softgaze.open", () => openSoftgaze(context.extensionUri)),
    vscode.commands.registerCommand("softgaze.toggle", () => backToSoftgaze(context.extensionUri)),
    vscode.workspace.onDidChangeConfiguration((e) => {
      if (e.affectsConfiguration("softgaze")) {
        SoftgazePanel.refresh();
      }
    }),
    vscode.window.onDidChangeWindowState(report),
    vscode.workspace.onDidChangeWorkspaceFolders(report),
    SoftgazePanel.onDidChangeActive(report),
  );
  report();
}

export function deactivate(): Promise<void> {
  return reportWindow(true);
}

/**
 * Tells softgaze whether this window has the keyboard, its folders and
 * whether its Softgaze panel is the active editor. macOS gives softgaze
 * the app in front but not which of its windows: this is how the minutes
 * counter tells this project's window from another one. Kept in memory by
 * the server, never logged.
 */
async function reportWindow(closed: boolean): Promise<void> {
  const body = {
    window: windowId,
    focused: !closed && vscode.window.state.focused,
    folders: (vscode.workspace.workspaceFolders ?? []).filter((f) => f.uri.scheme === "file").map((f) => f.uri.fsPath),
    panel_active: !closed && SoftgazePanel.isActive(),
    closed,
  };
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 2000);
  try {
    await fetch(`${baseUrl()}/api/attention/editor`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
      signal: ctrl.signal,
    });
  } catch {
    // server not running: nothing to tell
  } finally {
    clearTimeout(timer);
  }
}

/**
 * Softgaze in a VS Code window of its own: the panel moved into a floating
 * window (not kept on top: Ctrl+Alt+J goes to VS Code and back), its editor
 * group locked so files you open never land beside it. VS Code versions
 * without floating windows keep it as an editor tab.
 */
async function openSoftgaze(extensionUri: vscode.Uri): Promise<void> {
  if (SoftgazePanel.show(extensionUri)) {
    return; // already open: just brought to the front
  }
  try {
    await vscode.commands.executeCommand("workbench.action.moveEditorToNewWindow");
    await vscode.commands.executeCommand("workbench.action.lockEditorGroup");
  } catch {
    // Older VS Code: the panel stays where it opened.
  }
}

/**
 * Ctrl+Alt+J from VS Code: back to the Softgaze window you last used, wherever
 * it is (this window or a browser) and restored if minimized. Where
 * softgaze cannot raise windows (macOS), it asks the Softgaze panel open
 * in any VS Code window to come forward. When there is none, this
 * extension's Softgaze, opened if needed. In Softgaze the same key goes to VS
 * Code: the page handles it, since keys typed there never reach VS Code.
 */
async function backToSoftgaze(extensionUri: vscode.Uri): Promise<void> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 2000);
  try {
    const r = await fetch(`${baseUrl()}/api/follow/back`, { method: "POST", signal: ctrl.signal });
    if (r.ok) {
      return;
    }
  } catch {
    // server not running: open the panel, which says so
  } finally {
    clearTimeout(timer);
  }
  await openSoftgaze(extensionUri);
}
