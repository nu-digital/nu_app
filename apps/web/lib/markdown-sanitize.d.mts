import type { PluggableList } from "unified";

export const YOUTUBE_ID: RegExp;
export const sanitizeSchema: Record<string, unknown>;
export const sanitizePlugins: PluggableList;
export function rehypeSafeUrls(): (tree: unknown) => void;
export function rehypeSingleDataAttr(): (tree: unknown) => void;
