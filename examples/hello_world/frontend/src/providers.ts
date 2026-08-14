import type { HandlerContext } from "@cerasos/intercall";

let localeOverride: string | undefined;

export function setLocaleOverride(locale: string | undefined): void {
    localeOverride = locale;
}

/**
 * Returns the locale selected by the user, or the browser locale by default.
 * @intercall procedure locale
 * @return The browser's effective locale.
 */
export async function locale(_context: HandlerContext): Promise<string> {
    return localeOverride ?? navigator.language;
}
