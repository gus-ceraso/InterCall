import { InvalidArgumentError } from "../runtime/errors.js";

export function resolveWebSocketURL(input: string | URL, baseURL?: string | URL): URL {
    let url: URL;
    try {
        if (input instanceof URL) url = new URL(input.href);
        else {
            try {
                url = new URL(input);
            } catch {
                const base = baseURL ?? document.baseURI;
                url = new URL(input, base);
            }
        }
    } catch (error) {
        throw new InvalidArgumentError("invalid WebSocket URL", { cause: error });
    }
    if (url.protocol === "http:") url.protocol = "ws:";
    else if (url.protocol === "https:") url.protocol = "wss:";
    else if (url.protocol !== "ws:" && url.protocol !== "wss:") {
        throw new InvalidArgumentError("WebSocket URL must use ws:, wss:, http:, or https:");
    }
    return url;
}
