import log from 'electron-log/main';
import type { LogMessage, MainLogger } from 'electron-log';
import { redactSecretsDeep, redactSecretsInString } from './log-redaction';

export const DEFAULT_LOG_FILE_MAX_SIZE_BYTES = 5 * 1024 * 1024;

export interface MainLoggingOptions {
  debug: boolean;
  maxFileSizeBytes?: number;
  consoleTarget?: Partial<Console>;
}

export function redactionHook(message: LogMessage): LogMessage {
  return {
    ...message,
    data: message.data.map((entry) =>
      typeof entry === 'string' ? redactSecretsInString(entry) : redactSecretsDeep(entry),
    ),
  };
}

export function initMainLogging(options: MainLoggingOptions, logger: MainLogger = log): void {
  logger.transports.file.level = options.debug ? 'debug' : 'info';
  logger.transports.file.maxSize = options.maxFileSizeBytes ?? DEFAULT_LOG_FILE_MAX_SIZE_BYTES;
  logger.hooks.push(redactionHook);
  logger.errorHandler.startCatching({ showDialog: false });
  Object.assign(options.consoleTarget ?? console, logger.functions);
}

export function setDebugFileLogging(enabled: boolean, logger: MainLogger = log): void {
  logger.transports.file.level = enabled ? 'debug' : 'info';
}

export function isDebugFileLogging(logger: MainLogger = log): boolean {
  return logger.transports.file.level === 'debug';
}

export function mainLogFilePath(logger: MainLogger = log): string {
  return logger.transports.file.getFile().path;
}
