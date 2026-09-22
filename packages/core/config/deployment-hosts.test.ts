import { describe, expect, it } from 'vitest';
import {
  configStore,
  deploymentHostLabel,
  deploymentHostsFromMcpOverlay,
  EMPTY_DEPLOYMENT_HOSTS,
  mergeDeploymentHosts,
  parseDeploymentHosts,
} from './index';

describe('parseDeploymentHosts', () => {
  it('reads the whole field set', () => {
    expect(
      parseDeploymentHosts({
        jiraUrl: 'https://jira.example.test',
        confluenceUrl: 'https://wiki.example.test',
        ewsUrl: 'https://mail.example.test/EWS/Exchange.asmx',
        mailDomain: 'example.test',
        llmApiBase: 'https://llm.example.test/v1',
        llmModel: 'openai/coding-medium',
      }),
    ).toEqual({
      jiraUrl: 'https://jira.example.test',
      confluenceUrl: 'https://wiki.example.test',
      ewsUrl: 'https://mail.example.test/EWS/Exchange.asmx',
      mailDomain: 'example.test',
      llmApiBase: 'https://llm.example.test/v1',
      llmModel: 'openai/coding-medium',
    });
  });

  it('trims operator whitespace', () => {
    expect(parseDeploymentHosts({ mailDomain: '  example.test  ' }).mailDomain).toBe(
      'example.test',
    );
  });

  it('drops non-string fields instead of failing the parse', () => {
    const hosts = parseDeploymentHosts({
      jiraUrl: 42,
      mailDomain: 'example.test',
    });
    expect(hosts.jiraUrl).toBe('');
    expect(hosts.mailDomain).toBe('example.test');
  });

  it('treats absent, null and array payloads as an empty field set', () => {
    expect(parseDeploymentHosts(undefined)).toEqual(EMPTY_DEPLOYMENT_HOSTS);
    expect(parseDeploymentHosts(null)).toEqual(EMPTY_DEPLOYMENT_HOSTS);
    expect(parseDeploymentHosts([])).toEqual(EMPTY_DEPLOYMENT_HOSTS);
  });
});

describe('mergeDeploymentHosts', () => {
  it('lets a non-empty patch field win and keeps the base otherwise', () => {
    const merged = mergeDeploymentHosts(
      { ...EMPTY_DEPLOYMENT_HOSTS, jiraUrl: 'https://a.example.test', mailDomain: 'a.test' },
      { ...EMPTY_DEPLOYMENT_HOSTS, jiraUrl: 'https://b.example.test' },
    );
    expect(merged.jiraUrl).toBe('https://b.example.test');
    expect(merged.mailDomain).toBe('a.test');
  });
});

describe('deploymentHostsFromMcpOverlay', () => {
  it('reuses the addresses a deployment already stated in its MCP overlay', () => {
    const hosts = deploymentHostsFromMcpOverlay({
      mcpServers: {
        atlassian: {
          env: {
            JIRA_URL: 'https://jira.example.test',
            CONFLUENCE_URL: 'https://wiki.example.test',
          },
        },
        outlook: { env: { EWS_SERVER_URL: 'https://mail.example.test/EWS/Exchange.asmx' } },
      },
    });
    expect(hosts.jiraUrl).toBe('https://jira.example.test');
    expect(hosts.confluenceUrl).toBe('https://wiki.example.test');
    expect(hosts.ewsUrl).toBe('https://mail.example.test/EWS/Exchange.asmx');
    expect(hosts.llmApiBase).toBe('');
  });

  it('yields an empty field set for a malformed overlay', () => {
    expect(deploymentHostsFromMcpOverlay(null)).toEqual(EMPTY_DEPLOYMENT_HOSTS);
    expect(deploymentHostsFromMcpOverlay({ mcpServers: 'nope' })).toEqual(EMPTY_DEPLOYMENT_HOSTS);
  });
});

describe('deploymentHostLabel', () => {
  it('names the host a user has to open', () => {
    expect(deploymentHostLabel('https://jira.example.test/browse')).toBe('jira.example.test');
  });

  it('accepts a bare hostname', () => {
    expect(deploymentHostLabel('jira.example.test')).toBe('jira.example.test');
  });

  it('returns empty for absent or unparseable values, so callers show neutral copy', () => {
    expect(deploymentHostLabel('')).toBe('');
    expect(deploymentHostLabel('   ')).toBe('');
    expect(deploymentHostLabel('http://')).toBe('');
  });
});

describe('configStore deployment hosts (issue #493)', () => {
  it('defaults to an empty field set', () => {
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
    expect(configStore.getState().deploymentHosts).toEqual(EMPTY_DEPLOYMENT_HOSTS);
  });

  it('composes the three sources regardless of the order they arrive in', () => {
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
    configStore.getState().setDeploymentHosts({
      jiraUrl: 'https://jira.example.test',
      llmApiBase: 'https://llm.example.test/v1',
    });
    configStore.getState().setMcpPresetOverlay({
      mcpServers: {
        atlassian: {
          env: {
            JIRA_URL: 'https://stale.example.test',
            CONFLUENCE_URL: 'https://wiki.example.test',
          },
        },
      },
    });
    const hosts = configStore.getState().deploymentHosts;
    expect(hosts.jiraUrl).toBe('https://jira.example.test');
    expect(hosts.confluenceUrl).toBe('https://wiki.example.test');
    expect(hosts.llmApiBase).toBe('https://llm.example.test/v1');
    configStore.getState().setMcpPresetOverlay(null);
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
  });

  it('ignores a malformed payload rather than blanking what is known', () => {
    configStore.setState({
      deploymentHosts: { ...EMPTY_DEPLOYMENT_HOSTS, mailDomain: 'example.test' },
    });
    configStore.getState().setDeploymentHosts('nonsense');
    expect(configStore.getState().deploymentHosts.mailDomain).toBe('example.test');
    configStore.setState({ deploymentHosts: EMPTY_DEPLOYMENT_HOSTS });
  });
});
