import * as vscode from 'vscode';
import * as path from 'path';

export interface GebwebHost {
    shouldUseWsl(exe: string): boolean;
    toWslPath(p: string): string;
    quote(value: string): string;
}

const TEMPLATE_LANGUAGE = 'gebweb-template';
const GENERATE_KINDS = ['controller', 'dto', 'repository', 'resource', 'client'];
const MIGRATE_ACTIONS = ['up', 'down', 'status', 'create'];

export function activateGebweb(context: vscode.ExtensionContext, host: GebwebHost): void {
    const manifests = new ManifestCache();
    const classify = (doc: vscode.TextDocument) => { void applyTemplateLanguage(doc, manifests); };
    vscode.workspace.textDocuments.forEach(classify);
    context.subscriptions.push(
        vscode.workspace.onDidOpenTextDocument(classify),
        vscode.workspace.onDidSaveTextDocument(doc => {
            if (path.basename(doc.uri.path) === 'geblang.yaml') {
                manifests.clear();
                vscode.workspace.textDocuments.forEach(classify);
            }
        }),
        vscode.workspace.onDidChangeConfiguration(e => {
            if (e.affectsConfiguration('geblang.gebweb.templateDirs')) {
                vscode.workspace.textDocuments.forEach(classify);
            }
        }),
        vscode.commands.registerCommand('geblang.gebweb.newProject', () => newProject(host)),
        vscode.commands.registerCommand('geblang.gebweb.dev', () => runInProject(host, manifests, 'dev', ['dev'])),
        vscode.commands.registerCommand('geblang.gebweb.routes', () => runInProject(host, manifests, 'routes', ['routes'])),
        vscode.commands.registerCommand('geblang.gebweb.build', () => runInProject(host, manifests, 'build', ['build'])),
        vscode.commands.registerCommand('geblang.gebweb.worker', () => runInProject(host, manifests, 'worker', ['worker'])),
        vscode.commands.registerCommand('geblang.gebweb.generate', () => generate(host, manifests)),
        vscode.commands.registerCommand('geblang.gebweb.migrate', () => migrate(host, manifests)),
    );
}

// A manifest names a Gebweb project when it is gebweb itself or lists a gebweb dependency.
export function manifestUsesGebweb(text: string): boolean {
    if (/^name:\s*["']?gebweb["']?\s*$/m.test(text)) {
        return true;
    }
    const block = /^dependencies:\s*\n((?:[ \t]+.*\n?|\s*\n)*)/m.exec(text);
    return block !== null && /gebweb/i.test(block[1]);
}

export function isTemplatePath(relative: string, templateDirs: string[]): boolean {
    const parts = relative.split(/[\\/]/).filter(p => p.length > 0);
    if (parts.length < 2 || !parts[parts.length - 1].toLowerCase().endsWith('.html')) {
        return false;
    }
    const dirs = parts.slice(0, -1).join('/');
    return templateDirs.some(dir => {
        const normalized = dir.replace(/\\/g, '/').replace(/^\.\//, '').replace(/\/+$/, '');
        return normalized.length > 0 && (dirs === normalized || dirs.startsWith(normalized + '/'));
    });
}

class ManifestCache {
    private roots = new Map<string, vscode.Uri | null>();

    clear(): void {
        this.roots.clear();
    }

    async gebwebRoot(uri: vscode.Uri): Promise<vscode.Uri | undefined> {
        const folder = vscode.workspace.getWorkspaceFolder(uri);
        if (!folder) {
            return undefined;
        }
        let dir = vscode.Uri.joinPath(uri, '..');
        while (dir.path.startsWith(folder.uri.path)) {
            const key = dir.toString();
            if (!this.roots.has(key)) {
                this.roots.set(key, await readGebwebManifest(dir));
            }
            const found = this.roots.get(key);
            if (found) {
                return found;
            }
            if (dir.path === folder.uri.path) {
                break;
            }
            dir = vscode.Uri.joinPath(dir, '..');
        }
        return undefined;
    }

    async anyRoot(): Promise<vscode.Uri | undefined> {
        const active = vscode.window.activeTextEditor?.document.uri;
        if (active) {
            const root = await this.gebwebRoot(active);
            if (root) {
                return root;
            }
        }
        const found = await vscode.workspace.findFiles('**/geblang.yaml', '**/{node_modules,vendor,build}/**', 50);
        const roots: vscode.Uri[] = [];
        for (const manifest of found) {
            const dir = vscode.Uri.joinPath(manifest, '..');
            if (await readGebwebManifest(dir)) {
                roots.push(dir);
            }
        }
        if (roots.length <= 1) {
            return roots[0];
        }
        const pick = await vscode.window.showQuickPick(
            roots.map(r => ({ label: vscode.workspace.asRelativePath(r), root: r })),
            { placeHolder: 'Select the Gebweb project' },
        );
        return pick?.root;
    }
}

async function readGebwebManifest(dir: vscode.Uri): Promise<vscode.Uri | null> {
    try {
        const bytes = await vscode.workspace.fs.readFile(vscode.Uri.joinPath(dir, 'geblang.yaml'));
        return manifestUsesGebweb(Buffer.from(bytes).toString('utf8')) ? dir : null;
    } catch {
        return null;
    }
}

async function applyTemplateLanguage(doc: vscode.TextDocument, manifests: ManifestCache): Promise<void> {
    if (doc.languageId !== 'html' || doc.isUntitled) {
        return;
    }
    const root = await manifests.gebwebRoot(doc.uri);
    if (!root) {
        return;
    }
    const dirs = vscode.workspace.getConfiguration('geblang').get<string[]>('gebweb.templateDirs', ['templates']);
    const relative = doc.uri.path.slice(root.path.length + 1);
    if (isTemplatePath(relative, dirs)) {
        await vscode.languages.setTextDocumentLanguage(doc, TEMPLATE_LANGUAGE);
    }
}

function gebwebExecutable(): string {
    return vscode.workspace.getConfiguration('geblang').get<string>('gebweb.executablePath', 'gebweb');
}

function runGebweb(host: GebwebHost, label: string, cwd: vscode.Uri, args: string[]): void {
    const exe = gebwebExecutable();
    const argString = args.map(host.quote).join(' ');
    if (host.shouldUseWsl(exe)) {
        const terminal = vscode.window.createTerminal(`Gebweb: ${label}`);
        terminal.sendText(`wsl.exe --cd ${host.quote(host.toWslPath(cwd.fsPath))} -e ${exe} ${argString}`);
        terminal.show();
        return;
    }
    const terminal = vscode.window.createTerminal({ name: `Gebweb: ${label}`, cwd: cwd.fsPath });
    terminal.sendText(`${host.quote(exe)} ${argString}`);
    terminal.show();
}

async function runInProject(host: GebwebHost, manifests: ManifestCache, label: string, args: string[]): Promise<void> {
    const root = await manifests.anyRoot();
    if (!root) {
        vscode.window.showErrorMessage('Gebweb: no Gebweb project found (a geblang.yaml that depends on gebweb).');
        return;
    }
    runGebweb(host, label, root, args);
}

async function newProject(host: GebwebHost): Promise<void> {
    const name = await vscode.window.showInputBox({
        prompt: 'Name of the new Gebweb project',
        validateInput: value => /^[A-Za-z][A-Za-z0-9_-]*$/.test(value) ? undefined : 'Use letters, digits, - and _, starting with a letter.',
    });
    if (!name) {
        return;
    }
    const picked = await vscode.window.showOpenDialog({
        canSelectFiles: false,
        canSelectFolders: true,
        canSelectMany: false,
        defaultUri: vscode.workspace.workspaceFolders?.[0]?.uri,
        openLabel: 'Create project here',
    });
    if (!picked || picked.length === 0) {
        return;
    }
    runGebweb(host, 'new', picked[0], ['new', name]);
}

async function generate(host: GebwebHost, manifests: ManifestCache): Promise<void> {
    const root = await manifests.anyRoot();
    if (!root) {
        vscode.window.showErrorMessage('Gebweb: no Gebweb project found (a geblang.yaml that depends on gebweb).');
        return;
    }
    const kind = await vscode.window.showQuickPick(GENERATE_KINDS, { placeHolder: 'What to generate' });
    if (!kind) {
        return;
    }
    const args = ['generate', kind];
    if (kind === 'client') {
        const spec = await vscode.window.showOpenDialog({ canSelectMany: false, defaultUri: root, openLabel: 'Use OpenAPI spec', filters: { 'OpenAPI': ['yaml', 'yml', 'json'] } });
        if (!spec || spec.length === 0) {
            return;
        }
        args.push(path.relative(root.fsPath, spec[0].fsPath).replace(/\\/g, '/'));
    }
    const name = await vscode.window.showInputBox({
        prompt: `${kind} name (PascalCase)`,
        validateInput: value => /^[A-Z][A-Za-z0-9]*$/.test(value) ? undefined : 'Use PascalCase, e.g. Product.',
    });
    if (!name) {
        return;
    }
    args.push(name);
    runGebweb(host, `generate ${kind}`, root, args);
}

async function migrate(host: GebwebHost, manifests: ManifestCache): Promise<void> {
    const root = await manifests.anyRoot();
    if (!root) {
        vscode.window.showErrorMessage('Gebweb: no Gebweb project found (a geblang.yaml that depends on gebweb).');
        return;
    }
    const action = await vscode.window.showQuickPick(MIGRATE_ACTIONS, { placeHolder: 'Migration action' });
    if (!action) {
        return;
    }
    const args = ['migrate', action];
    if (action === 'create') {
        const name = await vscode.window.showInputBox({
            prompt: 'Migration name',
            validateInput: value => value.trim().length > 0 ? undefined : 'Enter a name, e.g. add_users.',
        });
        if (!name) {
            return;
        }
        args.push(name);
    }
    runGebweb(host, `migrate ${action}`, root, args);
}
