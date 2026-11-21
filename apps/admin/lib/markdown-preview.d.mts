import type { PluggableList } from "unified";

export const sanitizeSchema: Record<string, unknown>;
export const remarkPlugins: PluggableList;
export const rehypePlugins: PluggableList;
export function rehypeSafeUrls(): (tree: unknown) => void;
export function rehypeSingleDataAttr(): (tree: unknown) => void;
export function previewUrlTransform(url: string): string;
export function youtubeIds(text: string): string[];
