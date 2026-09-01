import ts from "typescript";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";

const safeSandbox =
  process.env.CWAPI_CODEX_SANDBOX === "workspace-write" ||
  process.env.NODE_OPTIONS?.includes("node-preload.cjs") === true;

function sandboxTypeScript(): Plugin {
  return {
    name: "cwapi-safe-typescript",
    enforce: "pre",
    config() {
      return { esbuild: false, keepProcessEnv: true, build: { minify: false } };
    },
    configResolved(config) {
      for (let index = config.plugins.length - 1; index >= 0; index -= 1) {
        if (config.plugins[index].name.startsWith("vite:esbuild")) {
          config.plugins.splice(index, 1);
        }
      }
    },
    transform(code, id) {
      const fileName = id.split("?", 1)[0];
      const transformed = code.replaceAll(
        "process.env.NODE_ENV",
        JSON.stringify(process.env.NODE_ENV || "production"),
      );
      if (fileName.includes("/node_modules/") || !/\.[cm]?[jt]sx?$/.test(fileName)) {
        return transformed === code ? null : { code: transformed, map: null };
      }
      const result = ts.transpileModule(transformed, {
        fileName,
        compilerOptions: {
          target: ts.ScriptTarget.ES2022,
          module: ts.ModuleKind.ESNext,
          jsx: ts.JsxEmit.ReactJSX,
          sourceMap: true,
        },
      });
      return {
        code: result.outputText,
        map: result.sourceMapText ? JSON.parse(result.sourceMapText) : null,
      };
    },
  };
}

export default defineConfig({
  plugins: [safeSandbox && sandboxTypeScript(), react()],
  esbuild: safeSandbox ? false : undefined,
  keepProcessEnv: safeSandbox ? true : undefined,
  build: {
    outDir: "dist",
    minify: safeSandbox ? false : undefined,
  },
});
