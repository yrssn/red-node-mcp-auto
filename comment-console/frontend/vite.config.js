import { defineConfig, loadEnv } from "vite";
import { fileURLToPath, URL } from "node:url";
import vue from "@vitejs/plugin-vue2";
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "VITE_");
  const apiTarget =
    process.env.VITE_API_TARGET ||
    env.VITE_API_TARGET ||
    "http://127.0.0.1:18061";
  return {
    plugins: [vue()],
    // Element UI 的 require 与应用 import 必须使用同一个 Vue 构造器。
    resolve: {
      alias: [
        {
          find: /^vue$/,
          replacement: fileURLToPath(
            new URL("./node_modules/vue/dist/vue.runtime.esm.js", import.meta.url),
          ),
        },
      ],
      dedupe: ["vue"],
    },
    base: "/console/",
    server: { proxy: { "/api": apiTarget } },
    build: { outDir: "dist", emptyOutDir: true },
  };
});
