import * as vscode from 'vscode';
import * as path from 'path';
import * as net from 'net';
import * as cp from 'child_process';
import {
    LanguageClient,
    LanguageClientOptions,
    ServerOptions,
    StreamInfo,
    TransportKind,
} from 'vscode-languageclient/node';

let client: LanguageClient | undefined;
let lspProcess: cp.ChildProcess | undefined;
let lspOutput: vscode.OutputChannel | undefined;
let lspStatus: vscode.StatusBarItem | undefined;

function getLspOutput(): vscode.OutputChannel {
    if (!lspOutput) {
        lspOutput = vscode.window.createOutputChannel('Geblang Language Server');
    }
    return lspOutput;
}

function geblangExecutablePath(): string {
    const raw = vscode.workspace.getConfiguration('geblang').get<string>('executablePath', 'geblang');
    return resolveExecutablePath(raw);
}

function executionMode(): string {
    return vscode.workspace.getConfiguration('geblang').get<string>('executionMode', 'auto');
}

// \\wsl.localhost\<distro>\home\... (or legacy \\wsl$\) -> /home/...
function wslUncToPosix(p: string): string | undefined {
    const match = /^\\\\wsl(?:\$|\.localhost)?\\[^\\]+\\(.+)$/.exec(p);
    return match ? '/' + match[1].replace(/\\/g, '/') : undefined;
}

function resolveExecutablePath(raw: string): string {
    return wslUncToPosix(raw) ?? raw;
}

// Windows-side file path (WSL UNC or C:\ drive) to its in-WSL form, for arguments passed into wsl.exe.
function toWslPath(p: string): string {
    const unc = wslUncToPosix(p);
    if (unc) {
        return unc;
    }
    const drive = /^([a-zA-Z]):[\\/]?(.*)$/.exec(p);
    if (drive) {
        const rest = drive[2].replace(/\\/g, '/');
        const mount = `/mnt/${drive[1].toLowerCase()}`;
        return rest ? `${mount}/${rest}` : mount;
    }
    return p;
}

// LSP runs inside WSL: strip the wsl authority+distro URI prefix outbound, restore it inbound so returned locations open on the Windows host.
let wslUriPrefix: { authority: string; distro: string } | undefined;

function code2ProtocolUri(uri: vscode.Uri): string {
    if (/^wsl(\$|\.localhost)$/i.test(uri.authority)) {
        const match = /^\/([^/]+)(\/.*)$/.exec(uri.path);
        if (match) {
            wslUriPrefix = { authority: uri.authority, distro: match[1] };
            return uri.with({ authority: '', path: match[2] }).toString();
        }
    }
    return uri.toString();
}

function protocol2CodeUri(value: string): vscode.Uri {
    const uri = vscode.Uri.parse(value);
    if (wslUriPrefix && uri.scheme === 'file' && !uri.authority) {
        return uri.with({ authority: wslUriPrefix.authority, path: `/${wslUriPrefix.distro}${uri.path}` });
    }
    return uri;
}

function shouldUseWsl(exe: string): boolean {
    const mode = executionMode();
    if (mode === 'wsl') {
        return true;
    }
    if (mode === 'native') {
        return false;
    }
    return process.platform === 'win32' && exe.startsWith('/');
}

function spawnArgs(subcommand: string, overrideExecutable?: string): { command: string; args: string[] } {
    const exe = resolveExecutablePath(overrideExecutable || geblangExecutablePath());
    if (shouldUseWsl(exe)) {
        return { command: 'wsl.exe', args: ['-e', exe, subcommand] };
    }
    return { command: exe, args: [subcommand] };
}

// Spawn a geblang subcommand with --tcp and return the address it prints to
// stdout. The server prints "IP:PORT\n" so that the caller connects to the
// correct interface - important for WSL2 where the server's 127.0.0.1 is not
// the same loopback as the Windows host's 127.0.0.1.
function spawnTcpServer(command: string, args: string[]): Promise<{ host: string; port: number; childProcess: cp.ChildProcess }> {
    return new Promise((resolve, reject) => {
        const childProcess = cp.spawn(command, [...args, '--tcp'], { stdio: ['ignore', 'pipe', 'pipe'] });
        let buf = '';
        childProcess.stdout!.on('data', (chunk: Buffer) => {
            buf += chunk.toString();
            const nl = buf.indexOf('\n');
            if (nl !== -1) {
                const addr = buf.slice(0, nl).trim();
                const lastColon = addr.lastIndexOf(':');
                if (lastColon !== -1) {
                    const host = addr.slice(0, lastColon);
                    const port = parseInt(addr.slice(lastColon + 1), 10);
                    if (!isNaN(port)) {
                        resolve({ host, port, childProcess });
                        return;
                    }
                }
                reject(new Error(`invalid address from server: ${addr}`));
            }
        });
        childProcess.on('error', reject);
        childProcess.on('exit', (code) => {
            if (code !== 0) {
                reject(new Error(`server exited with code ${code}`));
            }
        });
    });
}

async function startClient(): Promise<LanguageClient> {
    const exe = resolveExecutablePath(geblangExecutablePath());
    const { command, args } = spawnArgs('lsp');

    let serverOptions: ServerOptions;
    if (shouldUseWsl(exe)) {
        const { host, port, childProcess } = await spawnTcpServer(command, args);
        lspProcess?.kill();
        lspProcess = childProcess;
        serverOptions = (): Promise<StreamInfo> => new Promise((resolve, reject) => {
            const socket = net.createConnection({ port, host });
            socket.on('connect', () => resolve({ reader: socket, writer: socket }));
            socket.on('error', reject);
        });
    } else {
        serverOptions = {
            run:   { command, args, transport: TransportKind.stdio },
            debug: { command, args, transport: TransportKind.stdio },
        };
    }

    const outputChannel = getLspOutput();
    const clientOptions: LanguageClientOptions = {
        documentSelector: [{ scheme: 'file', language: 'geblang' }],
        synchronize: {
            fileEvents: vscode.workspace.createFileSystemWatcher('**/*.gb'),
        },
        uriConverters: {
            code2Protocol: code2ProtocolUri,
            protocol2Code: protocol2CodeUri,
        },
        outputChannel,
        traceOutputChannel: outputChannel,
    };
    const c = new LanguageClient('geblang', 'Geblang Language Server', serverOptions, clientOptions);
    c.start();
    setLspStatus('running');
    return c;
}

function ensureLspStatus(): vscode.StatusBarItem {
    if (!lspStatus) {
        lspStatus = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 99);
        lspStatus.command = 'geblang.showLspOutput';
        lspStatus.tooltip = 'Geblang Language Server (click to show log)';
        lspStatus.show();
    }
    return lspStatus;
}

function setLspStatus(state: 'starting' | 'running' | 'error' | 'stopped'): void {
    const status = ensureLspStatus();
    switch (state) {
        case 'starting':
            status.text = '$(loading~spin) Geblang LSP';
            return;
        case 'running':
            status.text = '$(check) Geblang LSP';
            return;
        case 'error':
            status.text = '$(error) Geblang LSP';
            return;
        case 'stopped':
            status.text = '$(circle-slash) Geblang LSP';
            return;
    }
}

export function activate(context: vscode.ExtensionContext): void {
    // ---- Status bar (always shown, independent of LSP) ----
    const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Right, 100);
    statusBar.command = 'geblang.selectExecutablePath';
    statusBar.tooltip = 'Select geblang executable';
    function updateStatusBar(): void {
        const exe = geblangExecutablePath();
        statusBar.text = `$(tools) geblang: ${path.basename(exe)}`;
        statusBar.show();
    }
    updateStatusBar();
    context.subscriptions.push(statusBar);

    // ---- Browse command ----
    const browseCmd = vscode.commands.registerCommand('geblang.selectExecutablePath', async () => {
        const uris = await vscode.window.showOpenDialog({
            canSelectFiles: true,
            canSelectFolders: false,
            canSelectMany: false,
            openLabel: 'Select geblang executable',
        });
        if (!uris || uris.length === 0) {
            return;
        }
        const picked = resolveExecutablePath(uris[0].fsPath);
        await vscode.workspace.getConfiguration('geblang').update(
            'executablePath', picked, vscode.ConfigurationTarget.Global
        );
    });
    context.subscriptions.push(browseCmd);

    // ---- Quick commands: Run, REPL, Doctor, Build, Clean, Restart LSP ----
    context.subscriptions.push(
        vscode.commands.registerCommand('geblang.runFile', () => runCurrentFile()),
        vscode.commands.registerCommand('geblang.openRepl', () => openRepl()),
        vscode.commands.registerCommand('geblang.runDoctor', () => runInTerminal('doctor', ['doctor'])),
        vscode.commands.registerCommand('geblang.buildProject', () => runInTerminal('build', ['build'])),
        vscode.commands.registerCommand('geblang.cleanCache', () => runInTerminal('cache clean', ['cache', 'clean'])),
        vscode.commands.registerCommand('geblang.restartLanguageServer', async () => {
            // Kill the running LSP subprocess and start a fresh one. Useful
            // after rebuilding the geblang binary - the LSP would otherwise
            // keep running the OLD binary until VS Code itself is reloaded.
            await client?.stop();
            lspProcess?.kill();
            lspProcess = undefined;
            client = undefined;
            launchLsp();
            vscode.window.showInformationMessage('Geblang: language server restarted');
        }),
        vscode.commands.registerCommand('geblang.showLspOutput', () => {
            getLspOutput().show(true);
        }),
    );

    // ---- Document formatter via `geblang fmt` ----
    context.subscriptions.push(
        vscode.languages.registerDocumentFormattingEditProvider({ language: 'geblang' }, {
            provideDocumentFormattingEdits(document: vscode.TextDocument): vscode.ProviderResult<vscode.TextEdit[]> {
                return formatDocument(document);
            },
        })
    );

    // ---- Code lenses: Run / Debug on the top-level main() and @test methods ----
    context.subscriptions.push(
        vscode.languages.registerCodeLensProvider({ language: 'geblang' }, new GeblangCodeLensProvider())
    );

    // ---- Test Controller: discover and run @test methods ----
    const testController = vscode.tests.createTestController('geblang', 'Geblang');
    context.subscriptions.push(testController);
    setupTestController(testController);

    // ---- Debug Adapter (always registered, independent of LSP) ----
    context.subscriptions.push(
        vscode.debug.registerDebugConfigurationProvider('geblang', new GeblangDebugConfigurationProvider())
    );
    context.subscriptions.push(
        vscode.debug.registerDebugAdapterDescriptorFactory('geblang', new GeblangDebugAdapterFactory())
    );

    // ---- LSP client (async, failure shows an error but does not block activation) ----
    context.subscriptions.push({ dispose: () => { client?.stop(); lspProcess?.kill(); } });

    ensureLspStatus();
    function launchLsp(): void {
        setLspStatus('starting');
        startClient().then(c => {
            client = c;
            setLspStatus('running');
        }).catch(err => {
            setLspStatus('error');
            getLspOutput().appendLine(`failed to start LSP: ${err.message}`);
            vscode.window.showErrorMessage(`Geblang: language server failed to start - ${err.message}`);
        });
    }
    launchLsp();

    // ---- Restart LSP and refresh status bar when config changes ----
    context.subscriptions.push(
        vscode.workspace.onDidChangeConfiguration(async e => {
            if (!e.affectsConfiguration('geblang.executablePath') && !e.affectsConfiguration('geblang.executionMode')) {
                return;
            }
            updateStatusBar();
            await client?.stop();
            lspProcess?.kill();
            lspProcess = undefined;
            client = undefined;
            launchLsp();
        })
    );
}

// ---- Helpers for the new commands ----

function runCurrentFile(): void {
    const editor = vscode.window.activeTextEditor;
    if (!editor || editor.document.languageId !== 'geblang') {
        vscode.window.showErrorMessage('Geblang: open a .gb file to run.');
        return;
    }
    const filePath = editor.document.uri.fsPath;
    const terminal = vscode.window.createTerminal('Geblang: Run');
    const exe = geblangExecutablePath();
    if (shouldUseWsl(exe)) {
        terminal.sendText(`wsl.exe -e ${exe} ${quote(toWslPath(filePath))}`);
    } else {
        terminal.sendText(`${quote(exe)} ${quote(filePath)}`);
    }
    terminal.show();
}

function openRepl(): void {
    const terminal = vscode.window.createTerminal('Geblang: REPL');
    const exe = geblangExecutablePath();
    if (shouldUseWsl(exe)) {
        terminal.sendText(`wsl.exe -e ${exe}`);
    } else {
        terminal.sendText(quote(exe));
    }
    terminal.show();
}

function runInTerminal(label: string, args: string[]): void {
    const terminal = vscode.window.createTerminal(`Geblang: ${label}`);
    const exe = geblangExecutablePath();
    const argString = args.map(quote).join(' ');
    if (shouldUseWsl(exe)) {
        terminal.sendText(`wsl.exe -e ${exe} ${argString}`);
    } else {
        terminal.sendText(`${quote(exe)} ${argString}`);
    }
    terminal.show();
}

function quote(value: string): string {
    if (value.indexOf(' ') === -1) {
        return value;
    }
    return `"${value.replace(/"/g, '\\"')}"`;
}

// formatDocument shells out to `geblang fmt --stdin` and returns the
// diff as a single replace-everything edit.
async function formatDocument(document: vscode.TextDocument): Promise<vscode.TextEdit[]> {
    const exe = geblangExecutablePath();
    const useWsl = shouldUseWsl(exe);
    const clean = vscode.workspace.getConfiguration('geblang').get<boolean>('format.clean', false);
    return new Promise(resolve => {
        const command = useWsl ? 'wsl.exe' : exe;
        const fmtArgs = clean ? ['fmt', '--clean', '--stdin'] : ['fmt', '--stdin'];
        const args = useWsl ? ['-e', exe, ...fmtArgs] : fmtArgs;
        const proc = cp.spawn(command, args);
        let stdout = '';
        let stderr = '';
        proc.stdout.on('data', chunk => { stdout += chunk.toString(); });
        proc.stderr.on('data', chunk => { stderr += chunk.toString(); });
        proc.on('error', () => resolve([]));
        proc.on('close', code => {
            if (code !== 0 || stdout.length === 0) {
                if (stderr) {
                    vscode.window.showErrorMessage(`geblang fmt: ${stderr.trim()}`);
                }
                resolve([]);
                return;
            }
            const fullRange = new vscode.Range(
                document.positionAt(0),
                document.positionAt(document.getText().length),
            );
            resolve([vscode.TextEdit.replace(fullRange, stdout)]);
        });
        proc.stdin.end(document.getText());
    });
}

// ---- Code lens provider ----

class GeblangCodeLensProvider implements vscode.CodeLensProvider {
    provideCodeLenses(document: vscode.TextDocument): vscode.CodeLens[] {
        const lenses: vscode.CodeLens[] = [];
        const text = document.getText();
        const lines = text.split('\n');
        let inTestClass = false;
        let testClassName = '';
        for (let i = 0; i < lines.length; i++) {
            const line = lines[i];
            const trimmed = line.trim();
            // Top-level main() function.
            const mainMatch = /^func\s+main\s*\(/.exec(trimmed);
            if (mainMatch) {
                const range = new vscode.Range(i, 0, i, line.length);
                lenses.push(new vscode.CodeLens(range, {
                    title: '$(play) Run',
                    command: 'geblang.runFile',
                    arguments: [],
                }));
                lenses.push(new vscode.CodeLens(range, {
                    title: '$(debug-alt) Debug',
                    command: 'workbench.action.debug.start',
                }));
            }
            // Test class boundary tracking.
            const classMatch = /^class\s+(\w+)\s+extends\s+(?:test\.)?Test\b/.exec(trimmed);
            if (classMatch) {
                inTestClass = true;
                testClassName = classMatch[1];
                const range = new vscode.Range(i, 0, i, line.length);
                lenses.push(new vscode.CodeLens(range, {
                    title: `$(beaker) Run all tests in ${testClassName}`,
                    command: 'geblang.runFile',
                }));
                continue;
            }
            if (inTestClass && /^\}\s*$/.test(trimmed)) {
                inTestClass = false;
                testClassName = '';
            }
        }
        return lenses;
    }
}

// ---- Test controller ----

function setupTestController(controller: vscode.TestController): void {
    // Discover .gb test files when activated and on save.
    discoverTestFiles(controller);
    vscode.workspace.onDidSaveTextDocument(doc => {
        if (doc.languageId === 'geblang' && doc.uri.fsPath.endsWith('_test.gb')) {
            indexTestFile(controller, doc.uri);
        }
    });

    const runHandler = async (request: vscode.TestRunRequest, token: vscode.CancellationToken) => {
        const run = controller.createTestRun(request);
        const queue: vscode.TestItem[] = [];
        if (request.include) {
            request.include.forEach(t => queue.push(t));
        } else {
            controller.items.forEach(t => queue.push(t));
        }
        while (queue.length > 0 && !token.isCancellationRequested) {
            const item = queue.shift()!;
            if (item.children.size > 0) {
                item.children.forEach(c => queue.push(c));
                continue;
            }
            if (!item.uri) {
                continue;
            }
            run.started(item);
            const start = Date.now();
            const result = await runGeblangTestFile(item.uri.fsPath);
            const duration = Date.now() - start;
            const match = result.tests.find(t => t.name === item.label);
            if (!match) {
                run.skipped(item);
            } else if (match.passed) {
                run.passed(item, duration);
            } else {
                run.failed(item, new vscode.TestMessage(match.message || 'test failed'), duration);
            }
        }
        run.end();
    };
    controller.createRunProfile('Run', vscode.TestRunProfileKind.Run, runHandler, true);
}

async function discoverTestFiles(controller: vscode.TestController): Promise<void> {
    const files = await vscode.workspace.findFiles('**/*_test.gb', '**/node_modules/**');
    for (const uri of files) {
        await indexTestFile(controller, uri);
    }
}

async function indexTestFile(controller: vscode.TestController, uri: vscode.Uri): Promise<void> {
    const text = (await vscode.workspace.fs.readFile(uri)).toString();
    const lines = text.split('\n');
    const fileItem = controller.createTestItem(uri.toString(), path.basename(uri.fsPath), uri);
    let currentClass: vscode.TestItem | undefined;
    let pendingTest = false;
    for (let i = 0; i < lines.length; i++) {
        const trimmed = lines[i].trim();
        const classMatch = /^class\s+(\w+)\s+extends\s+(?:test\.)?Test\b/.exec(trimmed);
        if (classMatch) {
            currentClass = controller.createTestItem(
                uri.toString() + '::' + classMatch[1],
                classMatch[1],
                uri,
            );
            currentClass.range = new vscode.Range(i, 0, i, lines[i].length);
            fileItem.children.add(currentClass);
            continue;
        }
        if (trimmed === '@test') {
            pendingTest = true;
            continue;
        }
        const funcMatch = /^func\s+(\w+)\s*\(/.exec(trimmed);
        if (funcMatch && pendingTest && currentClass) {
            const methodItem = controller.createTestItem(
                currentClass.id + '::' + funcMatch[1],
                funcMatch[1],
                uri,
            );
            methodItem.range = new vscode.Range(i, 0, i, lines[i].length);
            currentClass.children.add(methodItem);
            pendingTest = false;
        } else if (funcMatch) {
            pendingTest = false;
        }
    }
    controller.items.add(fileItem);
}

interface TestCaseResult {
    name: string;
    passed: boolean;
    message?: string;
}

interface TestFileResult {
    tests: TestCaseResult[];
}

async function runGeblangTestFile(filePath: string): Promise<TestFileResult> {
    return new Promise(resolve => {
        const exe = geblangExecutablePath();
        const useWsl = shouldUseWsl(exe);
        const command = useWsl ? 'wsl.exe' : exe;
        const args = useWsl ? ['-e', exe, 'test', '--verbose', toWslPath(filePath)] : ['test', '--verbose', filePath];
        const proc = cp.spawn(command, args);
        let stdout = '';
        proc.stdout.on('data', chunk => { stdout += chunk.toString(); });
        proc.on('close', () => {
            const tests: TestCaseResult[] = [];
            for (const line of stdout.split('\n')) {
                const passMatch = /^\s+PASS\s+(\S+)\s*$/.exec(line);
                if (passMatch) {
                    tests.push({ name: passMatch[1], passed: true });
                    continue;
                }
                const failMatch = /^\s+FAIL\s+(\S+):\s*(.*)$/.exec(line);
                if (failMatch) {
                    tests.push({ name: failMatch[1], passed: false, message: failMatch[2] });
                }
            }
            resolve({ tests });
        });
    });
}

export function deactivate(): Thenable<void> | undefined {
    lspProcess?.kill();
    return client?.stop();
}

class GeblangDebugConfigurationProvider implements vscode.DebugConfigurationProvider {
    provideDebugConfigurations(folder: vscode.WorkspaceFolder | undefined): vscode.ProviderResult<vscode.DebugConfiguration[]> {
        const program = vscode.window.activeTextEditor?.document.languageId === 'geblang'
            ? '${file}'
            : '${workspaceFolder}/main.gb';
        return [{
            type: 'geblang',
            request: 'launch',
            name: 'Debug Geblang Script',
            program,
            cwd: folder?.uri.fsPath || '${workspaceFolder}',
            args: [],
            stopOnEntry: false,
        }];
    }

    resolveDebugConfiguration(
        folder: vscode.WorkspaceFolder | undefined,
        config: vscode.DebugConfiguration
    ): vscode.ProviderResult<vscode.DebugConfiguration> {
        config.type = config.type || 'geblang';
        config.request = config.request || 'launch';
        config.name = config.name || 'Debug Geblang Script';
        config.program = config.program || '${file}';
        config.cwd = config.cwd || folder?.uri.fsPath || path.dirname(vscode.window.activeTextEditor?.document.uri.fsPath || '');
        config.args = config.args || [];
        config.stopOnEntry = Boolean(config.stopOnEntry);
        return config;
    }
}

class GeblangDebugAdapterFactory implements vscode.DebugAdapterDescriptorFactory {
    async createDebugAdapterDescriptor(
        session: vscode.DebugSession,
        _executable: vscode.DebugAdapterExecutable | undefined
    ): Promise<vscode.DebugAdapterDescriptor> {
        const config = session.configuration;
        const exe = resolveExecutablePath(config.geblangPath || geblangExecutablePath());
        const { command, args } = spawnArgs('dap', config.geblangPath);
        if (shouldUseWsl(exe)) {
            const { host, port } = await spawnTcpServer(command, args);
            return new vscode.DebugAdapterServer(port, host);
        }
        return new vscode.DebugAdapterExecutable(command, args, { cwd: config.cwd });
    }
}
