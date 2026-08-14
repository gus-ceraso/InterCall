import { connectWebSocket } from "@cerasos/intercall/browser";
import {
    createClient as createBackendClient,
    importBinding as backendImportBinding,
} from "./generated/backend/binding_gen.js";
import { exportBinding as browserExportBinding } from "./generated/browser/binding_gen.js";
import { setLocaleOverride } from "./providers.js";
import "./style.css";

const form = requiredElement<HTMLFormElement>("#greeting-form");
const nameInput = requiredElement<HTMLInputElement>("#name");
const localeSelect = requiredElement<HTMLSelectElement>("#locale");
const submitButton = requiredElement<HTMLButtonElement>("#submit");
const status = requiredElement<HTMLParagraphElement>("#status");
const greetingOutput = requiredElement<HTMLOutputElement>("#greeting");

const browserDefaultOption = localeSelect.options.item(0);
if (browserDefaultOption !== null) {
    browserDefaultOption.textContent = `Browser default (${navigator.language})`;
}

async function start(): Promise<void> {
    try {
        const connection = await connectWebSocket("/intercall", {
            exportBinding: browserExportBinding,
            importBinding: backendImportBinding,
        });
        const backend = createBackendClient(connection);
        let connected = true;

        status.textContent = "Connected";
        status.classList.add("connected");
        submitButton.disabled = false;

        const requestGreeting = async (): Promise<void> => {
            const name = nameInput.value.trim();
            if (name.length === 0) {
                nameInput.setCustomValidity("Enter a name.");
                nameInput.reportValidity();
                return;
            }
            nameInput.setCustomValidity("");
            submitButton.disabled = true;
            status.textContent = "Go is asking the browser for its locale…";

            try {
                greetingOutput.value = await backend.hello(name);
                status.textContent = `Locale: ${localeSelect.value || navigator.language}`;
                status.classList.add("connected");
            } catch (error: unknown) {
                greetingOutput.value = "";
                status.textContent = `Call failed: ${formatError(error)}`;
                status.classList.remove("connected");
            } finally {
                submitButton.disabled = !connected;
            }
        };

        form.addEventListener("submit", (event) => {
            event.preventDefault();
            void requestGreeting();
        });
        localeSelect.addEventListener("change", () => {
            setLocaleOverride(localeSelect.value || undefined);
            void requestGreeting();
        });
        window.addEventListener("pagehide", () => connection.close(), { once: true });
        void connection.closed.then((error) => {
            connected = false;
            submitButton.disabled = true;
            status.textContent = `Disconnected: ${error.message}`;
            status.classList.remove("connected");
        });

        await requestGreeting();
    } catch (error: unknown) {
        status.textContent = `Connection failed: ${formatError(error)}`;
        status.classList.remove("connected");
    }
}

function requiredElement<T extends Element>(selector: string): T {
    const element = document.querySelector<T>(selector);
    if (element === null) {
        throw new Error(`the hello-world page is missing ${selector}`);
    }
    return element;
}

function formatError(error: unknown): string {
    return error instanceof Error ? error.message : String(error);
}

void start();
