// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../../locales/en/common.json';
import enAgents from '../../../locales/en/agents.json';
import { McpServerDialog } from './mcp-server-dialog';

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };
const COPY = enAgents.tab_body.mcp_config;

function renderDialog(onSave = vi.fn().mockResolvedValue(undefined)) {
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <McpServerDialog
        open
        server={null}
        existingNames={new Set<string>()}
        onOpenChange={vi.fn()}
        onSave={onSave}
      />
    </I18nProvider>,
  );
  return { ...result, onSave };
}

async function fillJson(value: unknown) {
  fireEvent.change(screen.getByLabelText(/MCP server JSON configuration/i), {
    target: { value: JSON.stringify(value) },
  });
}

describe('McpServerDialog guidance', () => {
  beforeEach(() => vi.clearAllMocks());

  it('suggests an absolute path to an installed binary instead of a bare npx', () => {
    renderDialog();

    const placeholder = screen.getByLabelText('Command').getAttribute('placeholder');

    expect(placeholder).not.toBe('npx');
    expect(placeholder).toMatch(/^([/~]|[A-Za-z]:)/);
  });

  it('states that the machine must already have the program and that Node is not installed for the user', () => {
    renderDialog();

    expect(screen.getByText(COPY.dialog_command_hint)).toBeInTheDocument();
    expect(COPY.dialog_command_hint).toMatch(/absolute path/i);
    expect(COPY.dialog_command_hint).toMatch(/Node\.js/);
    expect(COPY.dialog_command_hint).toMatch(/installs nothing/i);
  });

  it('says plainly that the command is never checked on the runtime machine', () => {
    renderDialog();

    expect(screen.getByText(COPY.dialog_verification_note)).toBeInTheDocument();
    expect(COPY.dialog_verification_note).toMatch(/not (checked|detected)/i);
  });
});

describe('McpServerDialog validation', () => {
  beforeEach(() => vi.clearAllMocks());

  it('blocks a command that carries its own arguments', async () => {
    const user = userEvent.setup();
    const { onSave } = renderDialog();

    await user.type(screen.getByLabelText('Name'), 'files');
    await user.type(
      screen.getByLabelText('Command'),
      'npx -y @modelcontextprotocol/server-filesystem',
    );

    expect(screen.getByText(COPY.dialog_command_args_error)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /add server/i })).toBeDisabled();
    expect(onSave).not.toHaveBeenCalled();
  });

  it('blocks shell syntax the runtime never expands', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText('Name'), 'files');
    await user.type(screen.getByLabelText('Command'), '$HOME/bin/mcp');

    expect(screen.getByText(COPY.dialog_command_shell_error)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /add server/i })).toBeDisabled();
  });

  it('blocks a URL without an http scheme in JSON mode', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText('Name'), 'docs');
    await user.click(screen.getByRole('tab', { name: 'JSON' }));
    await fillJson({ type: 'http', url: 'mcp.example.com' });

    expect(screen.getByText(COPY.dialog_url_invalid)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /add server/i })).toBeDisabled();
  });

  it('saves a valid stdio configuration unchanged', async () => {
    const user = userEvent.setup();
    const { onSave } = renderDialog();

    await user.type(screen.getByLabelText('Name'), 'files');
    await user.type(screen.getByLabelText('Command'), '/usr/local/bin/mcp-server-filesystem');
    await user.click(screen.getByRole('button', { name: /add server/i }));

    expect(onSave).toHaveBeenCalledWith('files', {
      command: '/usr/local/bin/mcp-server-filesystem',
    });
  });

  it('saves a valid SSE configuration unchanged', async () => {
    const user = userEvent.setup();
    const { onSave } = renderDialog();

    await user.type(screen.getByLabelText('Name'), 'docs');
    await user.click(screen.getByRole('tab', { name: 'JSON' }));
    await fillJson({ type: 'sse', url: 'http://127.0.0.1:8931/sse' });
    await user.click(screen.getByRole('button', { name: /add server/i }));

    expect(onSave).toHaveBeenCalledWith('docs', {
      type: 'sse',
      url: 'http://127.0.0.1:8931/sse',
    });
  });
});

describe('McpServerDialog unverifiable commands', () => {
  beforeEach(() => vi.clearAllMocks());

  it('warns that a registry runner cannot start without public registry access, but still saves', async () => {
    const user = userEvent.setup();
    const { onSave } = renderDialog();

    await user.type(screen.getByLabelText('Name'), 'fetch');
    await user.type(screen.getByLabelText('Command'), 'npx');

    expect(screen.getByText(COPY.dialog_warning_registry)).toBeInTheDocument();
    expect(COPY.dialog_warning_registry).toMatch(/registry/i);
    expect(COPY.dialog_warning_registry).toMatch(/proxy|offline|no registry/i);

    const save = screen.getByRole('button', { name: /add server/i });
    expect(save).not.toBeDisabled();
    await user.click(save);

    expect(onSave).toHaveBeenCalledWith('fetch', { command: 'npx' });
  });

  it('marks a bare command as unverified rather than implying it works', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText('Name'), 'fetch');
    await user.type(screen.getByLabelText('Command'), 'mcp-server-fetch');

    expect(screen.getByText(COPY.dialog_warning_unverified)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /add server/i })).not.toBeDisabled();
  });

  it('drops the warning once the command is an absolute path', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.type(screen.getByLabelText('Name'), 'fetch');
    const command = screen.getByLabelText('Command');
    await user.type(command, 'mcp-server-fetch');
    expect(screen.getByText(COPY.dialog_warning_unverified)).toBeInTheDocument();

    await user.clear(command);
    await user.type(command, '/opt/mcp/bin/mcp-server-fetch');

    expect(screen.queryByText(COPY.dialog_warning_unverified)).not.toBeInTheDocument();
  });
});
