'use client';

import { useEffect, type ReactNode } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { getApi } from '../api';
import { ApiError } from '../api/client';
import { useAuthStore } from '../auth';
import {
  captureSignupSource,
  identify as identifyAnalytics,
  initAnalytics,
  resetAnalytics,
} from '../analytics';
import { configStore } from '../config';
import { workspaceKeys } from '../workspace/queries';
import { createLogger } from '../logger';
import { defaultStorage } from './storage';
import { setCurrentWorkspace } from './workspace-storage';
import type { ClientIdentity } from './types';
import type { StorageAdapter } from '../types/storage';
import type { User } from '../types';

const logger = createLogger('auth');

export function AuthInitializer({
  children,
  onLogin,
  onLogout,
  storage = defaultStorage,
  cookieAuth,
  identity,
}: {
  children: ReactNode;
  onLogin?: () => void;
  onLogout?: () => void;
  storage?: StorageAdapter;
  cookieAuth?: boolean;
  identity?: ClientIdentity;
}) {
  const qc = useQueryClient();

  useEffect(() => {
    const api = getApi();

    captureSignupSource();

    api
      .getConfig()
      .then((cfg) => {
        if (cfg.cdn_domain) {
          configStore.getState().setCdnConfig({
            cdnDomain: cfg.cdn_domain,
            cdnSigned: cfg.cdn_signed === true,
          });
        }
        configStore.getState().setAuthConfig({
          allowSignup: cfg.allow_signup,
          workspaceCreationDisabled: cfg.workspace_creation_disabled === true,
          vcsIntegrationAvailable: cfg.vcs_integration_available === true,
        });
        configStore.getState().setDaemonConfig({
          daemonServerUrl: cfg.daemon_server_url,
          daemonAppUrl: cfg.daemon_app_url,
        });
        configStore.getState().setImagePolicy({
          externalImages: cfg.external_images,
          imageHosts: cfg.image_hosts,
        });
        configStore.getState().setFeatureFlags(cfg.feature_flags);
        configStore.getState().setServerVersion(cfg.server_version);
        configStore.getState().setDeliveryProfile(cfg.delivery_profile);
        configStore.getState().setEmailTransport(cfg.email_transport);
        configStore.getState().setSkillSources(cfg.skill_sources);
        configStore.getState().setAllowedProviders(cfg.allowed_providers);
        configStore.getState().setDeploymentHosts(cfg.deployment_hosts);
        if (cfg.posthog_key) {
          initAnalytics({
            key: cfg.posthog_key,
            host: cfg.posthog_host || '',
            appVersion: identity?.version,
            environment: cfg.analytics_environment,
          });
        }
      })
      .catch(() => {
        /* config is optional — legacy file card matching degrades gracefully */
      });

    const onAuthSuccess = (user: User) => {
      onLogin?.();
      useAuthStore.setState({ user, isLoading: false });
      identifyAnalytics(user.id);
    };

    const onAuthFailure = () => {
      onLogout?.();
      resetAnalytics();
      useAuthStore.setState({ user: null, isLoading: false });
    };

    if (cookieAuth) {
      Promise.all([api.getMe(), api.listWorkspaces()])
        .then(([user, wsList]) => {
          onAuthSuccess(user);
          qc.setQueryData(workspaceKeys.list(), wsList);
        })
        .catch((err) => {
          logger.error('cookie auth init failed', err);
          if (err instanceof ApiError && err.status === 401) {
            onAuthFailure();
            return;
          }
          useAuthStore.setState({ isLoading: false });
        });
      return;
    }

    const token = storage.getItem('goosar_token');
    if (!token) {
      onLogout?.();
      useAuthStore.setState({ isLoading: false });
      return;
    }

    api.setToken(token);

    Promise.all([api.getMe(), api.listWorkspaces()])
      .then(([user, wsList]) => {
        onAuthSuccess(user);
        qc.setQueryData(workspaceKeys.list(), wsList);
      })
      .catch((err) => {
        logger.error('auth init failed', err);
        if (err instanceof ApiError && err.status === 401) {
          api.setToken(null);
          setCurrentWorkspace(null, null);
          storage.removeItem('goosar_token');
          onAuthFailure();
          return;
        }
        useAuthStore.setState({ isLoading: false });
      });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return <>{children}</>;
}
