import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

// An application that edits rich text with @riducms/plugin-richtext/editor and renders it on the
// server, with no Ridu-specific build setup.
export default defineConfig({
	plugins: [sveltekit()],
});
