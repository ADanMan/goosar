// Тонкая обёртка над expo-secure-store для токена авторизации; ключ совпадает
// с веб/десктопом.
import * as SecureStore from "expo-secure-store";

const TOKEN_KEY = "goosar_token";

export async function getToken(): Promise<string | null> {
  return SecureStore.getItemAsync(TOKEN_KEY);
}

export async function setToken(token: string): Promise<void> {
  await SecureStore.setItemAsync(TOKEN_KEY, token);
}

export async function clearToken(): Promise<void> {
  await SecureStore.deleteItemAsync(TOKEN_KEY);
}
