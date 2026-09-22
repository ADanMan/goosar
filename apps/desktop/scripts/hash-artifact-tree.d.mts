// Types for hash-artifact-tree.mjs so the Electron main process (TypeScript)
// can import the shared hasher with full types instead of an implicit `any`.
export declare const HASH_EXCLUDED_NAMES: Set<string>;
export declare function hashArtifactTree(root: string): Promise<string>;
