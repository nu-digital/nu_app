export function altFromFileName(name: string): string;
export function imageMarkdown(alt: string, url: string): string;
export function placeholderUrl(id: string): string;
export function insertBlock(markdown: string, pos: number, text: string): { markdown: string; caret: number };
export function replacePlaceholder(markdown: string, id: string, replacement: string): string;
export function removePlaceholder(markdown: string, id: string): string;
export function hasPlaceholder(markdown: string): boolean;
