// Isolated component-contract tests. Dependencies with effects are explicit stubs;
// these tests are not a claim of authenticated-browser or external-LLM validation.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import vm from "node:vm";
import test from "node:test";
import ts from "typescript";

const require = createRequire(import.meta.url);
const src = fileURLToPath(new URL("../", import.meta.url));
function load(relative, stubs) {
  const filename = path.join(src, relative);
  const code = ts.transpileModule(fs.readFileSync(filename, "utf8"), {
    fileName: filename,
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
    reportDiagnostics: true,
  });
  assert.equal(code.diagnostics?.filter(d => d.category === ts.DiagnosticCategory.Error).length ?? 0, 0);
  const mod = { exports: {} };
  const controlledRequire = name => {
    if (Object.hasOwn(stubs, name)) return stubs[name];
    if (["react", "react/jsx-runtime"].includes(name)) return require(name);
    throw Error(`Unstubbed dependency: ${name}`);
  };
  vm.runInNewContext(code.outputText, { module: mod, exports: mod.exports, require: controlledRequire }, { filename });
  return mod.exports;
}
const icons = { Monitor: "monitor", Moon: "moon", Sun: "sun", ArrowDownIcon: "arrow",
  ChevronLeftIcon: "left", ChevronRightIcon: "right", MoreHorizontalIcon: "more", SearchIcon: "search", CheckIcon: "check" };
const common = { "lucide-react": icons, "@/lib/utils": { cn: (...values) => values.filter(Boolean).join(" ") },
  "@/components/ui/button": { Button: "button" } };
function text(node) {
  if (node == null || typeof node === "boolean") return "";
  if (typeof node === "string" || typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(text).join("");
  return text(node.props?.children);
}

test("theme labels are Korean but persisted values and cycle stay light/dark/system", () => {
  const modes = ["light", "dark", "system"];
  for (const [index, mode] of modes.entries()) {
    const calls = [];
    const api = load("app/(main)/_components/sidebar/theme-switcher.tsx", {
      ...common,
      "@/lib/preferences/preferences-storage": { persistPreference: (key, value) => calls.push([key, value]) },
      "@/stores/preferences/preferences-provider": { usePreferencesStore: select => select({
        themeMode: mode, setThemeMode: value => calls.push(["set", value]),
      }) },
    });
    const button = api.ThemeSwitcher();
    assert.match(button.props["aria-label"], /현재 테마:/);
    assert.match(button.props["aria-label"], /라이트 모드|다크 모드|시스템 설정/);
    assert.doesNotMatch(button.props["aria-label"], /Current theme|light|dark|system/);
    button.props.onClick();
    assert.deepEqual(calls, [["set", modes[(index+1)%3]], ["theme_mode", modes[(index+1)%3]]]);
  }
});

test("command dialog default title and description are Korean; custom text remains supported", () => {
  const api = load("components/ui/command.tsx", { ...common, cmdk: { Command: {} },
    "@/components/ui/dialog": { Dialog: "dialog", DialogHeader: "header", DialogTitle: "title", DialogDescription: "description", DialogContent: "content" },
    "@/components/ui/input-group": { InputGroup: "group", InputGroupAddon: "addon" },
  });
  const output = text(api.CommandDialog({}));
  assert.match(output, /명령 팔레트/);
  assert.match(output, /실행할 명령을 검색하세요/);
  assert.doesNotMatch(output, /Command Palette|Search for a command/);
  assert.match(text(api.CommandDialog({title: "사용자 제목", description: "사용자 설명"})), /사용자 제목사용자 설명/);
});

test("pagination defaults and accessibility text are Korean, preserving custom labels", () => {
  const api = load("components/ui/pagination.tsx", common);
  const previous = api.PaginationPrevious({}), next = api.PaginationNext({});
  assert.equal(text(previous), "이전");
  assert.equal(text(next), "다음");
  assert.equal(previous.props["aria-label"], "이전 페이지로 이동");
  assert.equal(next.props["aria-label"], "다음 페이지로 이동");
  assert.equal(text(api.PaginationNext({text: "사용자 지정"})), "사용자 지정");
});

test("message scroller keeps direction identifiers while translating screen-reader instructions", () => {
  const api = load("components/ui/message-scroller.tsx", { ...common,
    "@shadcn/react/message-scroller": { MessageScroller: { Button: "scrollbutton" } },
  });
  for (const [direction, label] of [["end", "끝으로 스크롤"], ["start", "처음으로 스크롤"]]) {
    const output = api.MessageScrollerButton({direction});
    assert.equal(output.props.direction, direction);
    assert.equal(text(output), label);
  }
  assert.equal(text(api.MessageScrollerButton({children: "사용자 지정"})), "사용자 지정");
});

test("reviewed date/time expressions explicitly use ko-KR without changing stored timestamps", () => {
  const panel = fs.readFileSync(path.join(src, "components/side-question-workspace.tsx"), "utf8");
  const tasks = fs.readFileSync(path.join(src, "app/(main)/function/tasks/page.tsx"), "utf8");
  assert(panel.includes('new Date(side.snapshot.captured_at).toLocaleString("ko-KR")'));
  assert(panel.includes('new Date(item.snapshot_at).toLocaleString("ko-KR")'));
  assert(panel.includes('new Date(item.snapshot_at).toLocaleTimeString("ko-KR")'));
  assert(panel.includes("dateTime={item.snapshot_at}"));
  assert(tasks.includes('return date.toLocaleString("ko-KR");'));
});
