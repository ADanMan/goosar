/**
 * Типы интеграции с Git-провайдерами по токену (Forgejo, Gitea, GitLab). В
 * отличие от GitHub, здесь нет модели App/installation: каждый workspace
 * хранит токен-подключение к инстансу провайдера. Pull request'ы из этих
 * провайдеров отражаются общей формой GitHubPullRequest с полем `provider`.
 */

export type VCSProvider = 'forgejo' | 'gitea' | 'gitlab';

export interface VCSConnection {
  id: string;
  workspace_id: string;
  provider: VCSProvider;
  instance_url: string;
  account_login: string;
  webhook_url: string;
  webhook_path: string;
  created_at: string;
}

export interface ListVCSConnectionsResponse {
  connections: VCSConnection[];
  available?: boolean;
  configured?: boolean;
  can_manage?: boolean;
}

export interface ConnectVCSRequest {
  provider: VCSProvider;
  instance_url: string;
  access_token: string;
}

export interface ConnectVCSResponse extends VCSConnection {
  webhook_secret: string;
}
