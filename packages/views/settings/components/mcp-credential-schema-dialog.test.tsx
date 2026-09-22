// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { I18nProvider } from '@goosar/core/i18n/react';
import enCommon from '../../locales/en/common.json';
import enSettings from '../../locales/en/settings.json';
import {
  ATLASSIAN_PAT_PRESET_FIELDS,
  McpCredentialSchemaDialog,
} from './mcp-credential-schema-dialog';

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

function renderDialog(onSave = vi.fn()) {
  render(
    <I18nProvider resources={TEST_RESOURCES} locale="en">
      <McpCredentialSchemaDialog
        open
        server={{ id: 'srv-1', name: 'atlassian', credential_schema: [] }}
        saving={false}
        onOpenChange={() => {}}
        onSave={onSave}
      />
    </I18nProvider>,
  );
}

describe('McpCredentialSchemaDialog — Atlassian (PAT) preset (T-11, #639)', () => {
  it('fills exactly JIRA_PERSONAL_TOKEN and CONFLUENCE_PERSONAL_TOKEN when clicked', async () => {
    const user = userEvent.setup();
    renderDialog();

    await user.click(
      screen.getByRole('button', { name: enSettings.mcp.schema_preset_atlassian_pat }),
    );

    const keyInputs = screen.getAllByLabelText(
      enSettings.mcp.schema_key_label,
    ) as HTMLInputElement[];
    const keys = keyInputs.map((input) => input.value);
    expect(keys).toEqual(['JIRA_PERSONAL_TOKEN', 'CONFLUENCE_PERSONAL_TOKEN']);
  });

  it('does not duplicate fields already present', async () => {
    const user = userEvent.setup();
    const onSave = vi.fn();
    render(
      <I18nProvider resources={TEST_RESOURCES} locale="en">
        <McpCredentialSchemaDialog
          open
          server={{
            id: 'srv-1',
            name: 'atlassian',
            credential_schema: [
              { key: 'JIRA_PERSONAL_TOKEN', label: '', hint: '', required: true },
            ],
          }}
          saving={false}
          onOpenChange={() => {}}
          onSave={onSave}
        />
      </I18nProvider>,
    );

    await user.click(
      screen.getByRole('button', { name: enSettings.mcp.schema_preset_atlassian_pat }),
    );

    const keyInputs = screen.getAllByLabelText(
      enSettings.mcp.schema_key_label,
    ) as HTMLInputElement[];
    expect(keyInputs.map((input) => input.value)).toEqual([
      'JIRA_PERSONAL_TOKEN',
      'CONFLUENCE_PERSONAL_TOKEN',
    ]);
  });

  it('exports exactly the two PAT-only fields, no basic-auth pair', () => {
    expect(ATLASSIAN_PAT_PRESET_FIELDS.map((f) => f.key)).toEqual([
      'JIRA_PERSONAL_TOKEN',
      'CONFLUENCE_PERSONAL_TOKEN',
    ]);
    for (const bad of [
      'JIRA_USERNAME',
      'JIRA_API_TOKEN',
      'CONFLUENCE_USERNAME',
      'CONFLUENCE_API_TOKEN',
    ]) {
      expect(ATLASSIAN_PAT_PRESET_FIELDS.map((f) => f.key)).not.toContain(bad);
    }
  });
});
