import { safeStorage } from 'electron';

export interface SecretVault {
  isAvailable(): boolean;
  encrypt(plaintext: string): string | null;
  decrypt(ciphertext: string): string | null;
}

export const electronSecretVault: SecretVault = {
  isAvailable(): boolean {
    try {
      return safeStorage.isEncryptionAvailable();
    } catch {
      return false;
    }
  },
  encrypt(plaintext: string): string | null {
    try {
      if (!safeStorage.isEncryptionAvailable()) return null;
      return safeStorage.encryptString(plaintext).toString('base64');
    } catch {
      return null;
    }
  },
  decrypt(ciphertext: string): string | null {
    try {
      if (!safeStorage.isEncryptionAvailable()) return null;
      return safeStorage.decryptString(Buffer.from(ciphertext, 'base64'));
    } catch {
      return null;
    }
  },
};
