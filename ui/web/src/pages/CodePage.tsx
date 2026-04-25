import { useState } from 'react';

import type { ApiClient } from '../api/client';
import { DiffViewer } from '../components/DiffViewer';
import { ToolOutput } from '../components/ToolOutput';

interface CodePageProps {
  api: ApiClient;
}

const prompts = {
  inspect: (workspace: string) => `请检查项目结构和测试命令，workspace: ${workspace}`,
  search: (workspace: string, query: string) => `请在 workspace ${workspace} 搜索代码 \`${query}\``,
  read: (workspace: string, path: string) => `请读取文件 \`${path}\`，workspace: ${workspace}`,
  tests: (workspace: string) => `请检测并运行测试，workspace: ${workspace}`,
  fix: (workspace: string) => `请修复测试失败并进入有界修复循环，workspace: ${workspace}`,
  gitStatus: (workspace: string) => `请查看 git status，workspace: ${workspace}`,
  gitDiff: (workspace: string) => `请查看 git diff，workspace: ${workspace}`,
  diffSummary: (workspace: string) => `请总结 git diff，workspace: ${workspace}`,
  commitMessage: (workspace: string) => `请生成 commit message 建议但不要提交，workspace: ${workspace}`
};

export function CodePage({ api }: CodePageProps) {
  const [workspace, setWorkspace] = useState('.');
  const [query, setQuery] = useState('');
  const [path, setPath] = useState('');
  const [diff, setDiff] = useState('');
  const [lastResponse, setLastResponse] = useState<unknown>();
  const [error, setError] = useState('');

  const startWorkflow = async (content: string) => {
    setError('');
    try {
      const conversation = await api.createConversation('Code workflow');
      const response = await api.postMessage(conversation.id, content);
      setLastResponse(response);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="single-column">
      <section className="panel">
        <header className="panel-header">
          <div>
            <h2>Code Workspace</h2>
            <p>Code and Git actions are launched as agent workflow messages so ToolRouter, policy, approval, and audit remain in control.</p>
          </div>
        </header>
        {error ? <div className="error-banner">{error}</div> : null}
        <div className="form-grid">
          <label className="field">
            <span>Workspace</span>
            <input value={workspace} onChange={(event) => setWorkspace(event.target.value)} placeholder="/path/to/workspace" />
          </label>
          <label className="field">
            <span>Search query</span>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="symbol, text, failure" />
          </label>
          <label className="field">
            <span>File path</span>
            <input value={path} onChange={(event) => setPath(event.target.value)} placeholder="internal/..." />
          </label>
        </div>
        <div className="button-grid">
          <button type="button" className="button" onClick={() => startWorkflow(prompts.inspect(workspace))}>Inspect</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.search(workspace, query))} disabled={!query}>Search</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.read(workspace, path))} disabled={!path}>Read file</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.tests(workspace))}>Run tests</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.fix(workspace))}>Fix tests</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.gitStatus(workspace))}>Git status</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.gitDiff(workspace))}>Git diff</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.diffSummary(workspace))}>Diff summary</button>
          <button type="button" className="button" onClick={() => startWorkflow(prompts.commitMessage(workspace))}>Commit message</button>
        </div>
        <ToolOutput title="Last workflow response" output={lastResponse} defaultOpen />
      </section>

      <section className="panel">
        <header className="panel-header">
          <h2>Diff Preview</h2>
          <p>Paste backend-generated unified diff or patch preview. Applying patches still requires backend approval.</p>
        </header>
        <label className="field">
          <span>Unified diff</span>
          <textarea value={diff} onChange={(event) => setDiff(event.target.value)} rows={8} placeholder="diff --git ..." />
        </label>
        <DiffViewer diff={diff} />
      </section>
    </div>
  );
}

