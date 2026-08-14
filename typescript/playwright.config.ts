import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
    testDir: "./test/browser",
    fullyParallel: true,
    timeout: 15_000,
    use: { headless: true },
    projects: [
        { name: "chromium", use: { ...devices["Desktop Chrome"] } },
        { name: "firefox", use: { ...devices["Desktop Firefox"] } },
        { name: "webkit", use: { ...devices["Desktop Safari"] } },
    ],
});
