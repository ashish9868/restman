import "bootstrap/dist/css/bootstrap.min.css";
import "bootstrap-icons/font/bootstrap-icons.css";
import "./style.css";
import { EditorView, basicSetup } from "codemirror";
import { Decoration, ViewPlugin } from "@codemirror/view";
import { Annotation } from "@codemirror/state";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { xml } from "@codemirror/lang-xml";
import { html } from "@codemirror/lang-html";
import { php } from "@codemirror/lang-php";
import { StreamLanguage } from "@codemirror/language";
import { python } from "@codemirror/legacy-modes/mode/python";
import { go } from "@codemirror/legacy-modes/mode/go";
import { java, csharp, kotlin } from "@codemirror/legacy-modes/mode/clike";
import { ruby } from "@codemirror/legacy-modes/mode/ruby";
import { rust } from "@codemirror/legacy-modes/mode/rust";
import { swift } from "@codemirror/legacy-modes/mode/swift";
import { shell } from "@codemirror/legacy-modes/mode/shell";
import { powerShell } from "@codemirror/legacy-modes/mode/powershell";
import { oneDark } from "@codemirror/theme-one-dark";
import { EditorState } from "@codemirror/state";
import { autocompletion, completeFromList } from "@codemirror/autocomplete";

let gqlNames = [];
let snippetView = null;

const $ = (id) => document.getElementById(id);

function icon(name) {
  const el = document.createElement("i");
  el.className = `bi bi-${name}`;
  el.setAttribute("aria-hidden", "true");
  return el;
}
let snapshot = null;
let tabs = [];
let activeId = null;
let selectedId = null;
let dragId = null;
let suppressClick = false;
let sidebar = "collections";
let importKind = "collection";
const editors = {};
let wsTimer = null;

async function rpc(method, params = {}) {
  const res = await fetch("/api/rpc", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ method, params }),
  });
  const data = await res.json();
  if (!res.ok || data.error) throw new Error(data.error || res.statusText);
  return data.result;
}

function toast(error) {
  note(error.message || String(error), false);
}

function note(message, ok = false) {
  $("toast-text").textContent = message;
  $("toast").classList.toggle("ok", ok);
  $("toast").classList.remove("d-none");
  clearTimeout(note.timer);
  note.timer = setTimeout(() => $("toast").classList.add("d-none"), 2500);
}

async function guard(work) {
  try { return await work(); } catch (error) { toast(error); }
}

function blankRequest(name = "Untitled", protocol = "http") {
  return {
    id: crypto.randomUUID(), name, protocol, method: "GET", url: "",
    params: [], headers: [], variables: [],
    auth: { type: protocol === "websocket" ? "none" : "inherit" },
    body: { mode: "none", raw: "", language: "json", urlencoded: [], formdata: [], filePath: "", graphql: { query: "", variables: "", operationName: "" } },
    scripts: { prerequest: "", test: "" },
    options: {},
    examples: [],
  };
}

function normalize(req) {
  req.params ||= [];
  req.headers ||= [];
  req.variables ||= [];
  req.auth ||= { type: "inherit" };
  req.body ||= { mode: "none" };
  req.body.urlencoded ||= [];
  req.body.formdata ||= [];
  req.body.graphql ||= { query: "", variables: "", operationName: "" };
  req.scripts ||= { prerequest: "", test: "" };
  req.options ||= {};
  req.examples ||= [];
  req.protocol ||= "http";
  req.method ||= "GET";
  return req;
}

function active() { return tabs.find((tab) => tab.id === activeId); }

function applyTheme(theme) {
  const dark = theme === "dark" || (theme !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.bsTheme = dark ? "dark" : "light";
  const themeButton = $("theme");
  if (themeButton) {
    themeButton.replaceChildren(icon(dark ? "moon-stars" : "sun"));
    themeButton.title = dark ? "Dark theme" : "Light theme";
  }
  return dark;
}

function langOf(name) {
  if (name === "javascript") return javascript();
  if (name === "json") return json();
  if (name === "xml") return xml();
  if (name === "html") return html();
  return [];
}

const varRefresh = Annotation.define();
const dynamicVars = new Set(["$guid", "$timestamp", "$isoTimestamp", "$randomInt"]);

function eachVar(text, visit) {
  const re = /\{\{[^{}]*\}\}/g;
  let match;
  while ((match = re.exec(text))) visit(match);
}

function addVarNames(names, rows) {
  for (const row of rows || []) {
    if (row.enabled !== false && row.key) names.add(row.key.trim());
  }
}

function knownVarNames() {
  const names = new Set(dynamicVars);
  addVarNames(names, snapshot?.globals);
  const env = (snapshot?.environments || []).find((item) => item.id === snapshot?.activeEnvironmentId);
  addVarNames(names, env?.variables);
  const tab = active();
  addVarNames(names, tab?.request?.variables);
  let folderId = tab?.folderId;
  const seen = new Set();
  while (folderId && !seen.has(folderId)) {
    seen.add(folderId);
    addVarNames(names, findById(snapshot?.tree, folderId)?.variables);
    const parent = findParent(snapshot?.tree, folderId);
    folderId = parent?.kind === "folder" ? parent.id : "";
  }
  addVarNames(names, findById(snapshot?.tree, tab?.collectionId)?.variables);
  return names;
}

function varState(token) {
  return knownVarNames().has(token.slice(2, -2).trim()) ? "known" : "missing";
}

const varHighlight = ViewPlugin.fromClass(class {
  constructor(view) { this.decorations = varDecorations(view); }
  update(update) {
    const refresh = update.transactions.some((tr) => tr.annotation(varRefresh));
    if (update.docChanged || update.viewportChanged || refresh) this.decorations = varDecorations(update.view);
  }
}, { decorations: (plugin) => plugin.decorations });

function varDecorations(view) {
  const marks = [];
  for (const { from, to } of view.visibleRanges) {
    const text = view.state.doc.sliceString(from, to);
    eachVar(text, (match) => {
      marks.push(Decoration.mark({ class: `cm-var ${varState(match[0])}` }).range(from + match.index, from + match.index + match[0].length));
    });
  }
  return Decoration.set(marks);
}

function varNodes(text) {
  const nodes = [];
  const source = text || "";
  let last = 0;
  eachVar(source, (match) => {
    if (match.index > last) nodes.push(document.createTextNode(source.slice(last, match.index)));
    const token = document.createElement("span");
    token.className = `var-token ${varState(match[0])}`;
    token.textContent = match[0];
    nodes.push(token);
    last = match.index + match[0].length;
  });
  nodes.push(document.createTextNode(source.slice(last)));
  return nodes;
}

function refreshVarHighlights() {
  document.querySelectorAll("input, textarea").forEach((el) => el.refreshVars?.());
  for (const view of Object.values(editors)) view?.dispatch({ annotations: varRefresh.of(true) });
}

let bodyView = "pretty";
let zoomLevel = 1;
const collapsed = new Set(JSON.parse(localStorage.getItem("restman-collapsed") || "[]"));

function saveCollapsed() {
  localStorage.setItem("restman-collapsed", JSON.stringify([...collapsed]));
}

function lookupVar(name) {
  const dynamic = { $guid: "guid", $timestamp: "timestamp", $isoTimestamp: "iso", $randomInt: "int" };
  if (dynamic[name]) return `<${dynamic[name]}>`;
  const rows = [];
  const tab = active();
  rows.push(tab?.request?.variables);
  let folderId = tab?.folderId;
  const seen = new Set();
  while (folderId && !seen.has(folderId)) {
    seen.add(folderId);
    rows.push(findById(snapshot?.tree, folderId)?.variables);
    const parent = findParent(snapshot?.tree, folderId);
    folderId = parent?.kind === "folder" ? parent.id : "";
  }
  const env = (snapshot?.environments || []).find((item) => item.id === snapshot?.activeEnvironmentId);
  rows.push(env?.variables, findById(snapshot?.tree, tab?.collectionId)?.variables, snapshot?.globals);
  for (const list of rows) {
    const hit = (list || []).find((row) => row.enabled !== false && row.key === name);
    if (hit) return hit.value ?? "";
  }
  return null;
}

function resolvedText(url) {
  return (url || "").replace(/\{\{\s*([^{}]+?)\s*\}\}/g, (token, name) => {
    const value = lookupVar(name.trim());
    return value == null ? token : value;
  });
}

function pathKeys(url) {
  return [...(url || "").split("?")[0].matchAll(/:([A-Za-z_][\w-]*)/g)].map((match) => match[1]);
}

function parseQuery(url) {
  const query = ((url || "").split("?")[1] || "").split("#")[0];
  if (!query) return [];
  return query.split("&").filter(Boolean).map((pair) => {
    const cut = pair.indexOf("=");
    const key = decodeURIComponent(cut < 0 ? pair : pair.slice(0, cut));
    const value = decodeURIComponent((cut < 0 ? "" : pair.slice(cut + 1)).replace(/\+/g, " "));
    return { key, value, enabled: true, description: "query" };
  });
}

function ensureParams(req) {
  req.params ||= [];
  if (!req.params.length && (req.url || "").includes("?")) req.params = parseQuery(req.url);
  for (const key of pathKeys(req.url)) {
    if (!req.params.some((row) => row.key === key && row.description === "path")) {
      req.params.push({ key, value: "", enabled: true, description: "path" });
    }
  }
}

function writeQuery(req) {
  const base = (req.url || "").split("?")[0].split("#")[0];
  const query = (req.params || []).filter((row) => row.enabled !== false && row.key && row.description !== "path");
  req.url = query.length ? `${base}?${query.map((row) => `${encodeURIComponent(row.key)}=${encodeURIComponent(row.value || "")}`).join("&")}` : base;
}

function suggestFor(input) {
  input.addEventListener("input", () => {
    const box = document.getElementById("suggest");
    box?.remove();
    const text = input.value.slice(0, input.selectionStart || input.value.length);
    const open = text.lastIndexOf("{{");
    if (open < 0 || text.slice(open).includes("}}")) return;
    const prefix = text.slice(open + 2).toLowerCase();
    const names = [...knownVarNames()].filter((name) => name.toLowerCase().includes(prefix)).slice(0, 8);
    if (!names.length) return;
    const menu = document.createElement("div");
    menu.id = "suggest";
    menu.className = "suggest";
    const rect = input.getBoundingClientRect();
    menu.style.left = `${rect.left}px`;
    menu.style.top = `${rect.bottom + 2}px`;
    for (const name of names) {
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = name;
      button.addEventListener("mousedown", (event) => {
        event.preventDefault();
        const start = input.selectionStart || input.value.length;
        const before = input.value.slice(0, open);
        const after = input.value.slice(start);
        input.value = `${before}{{${name}}}${after}`;
        input.dispatchEvent(new Event("input", { bubbles: true }));
        menu.remove();
      });
      menu.append(button);
    }
    document.body.append(menu);
  });
}

function breadcrumb(tab) {
  const names = [];
  const collection = findById(snapshot?.tree, tab.collectionId);
  if (collection) names.push(collection.name);
  let folderId = tab.folderId;
  const chain = [];
  const seen = new Set();
  while (folderId && !seen.has(folderId)) {
    seen.add(folderId);
    const folder = findById(snapshot?.tree, folderId);
    if (!folder) break;
    chain.unshift(folder.name);
    const parent = findParent(snapshot?.tree, folderId);
    folderId = parent?.kind === "folder" ? parent.id : "";
  }
  names.push(...chain, tab.request.name || "Untitled");
  return names.join(" / ");
}

function requestsUnder(node) {
  if (!node) return [];
  if (node.kind === "request") return [node];
  return (node.children || []).flatMap(requestsUnder);
}

function dayLabel(value) {
  const date = new Date(value || 0);
  if (Number.isNaN(date.getTime())) return "Earlier";
  const today = new Date();
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const then = new Date(date.getFullYear(), date.getMonth(), date.getDate());
  const days = Math.round((start - then) / 86400000);
  if (days === 0) return "Today";
  if (days === 1) return "Yesterday";
  return date.toLocaleDateString();
}

function highlightVars(input) {
  if (input.varField) return input.varField;
  const wrap = document.createElement("span");
  wrap.className = "var-field";
  if (input.id === "url" || input.id === "ws-input") wrap.classList.add("grow");
  if (input.id === "req-name") wrap.classList.add("req-name-field");
  const mirror = document.createElement("div");
  mirror.className = "var-mirror";
  mirror.setAttribute("aria-hidden", "true");
  if (input.parentNode) input.parentNode.insertBefore(wrap, input);
  wrap.append(mirror, input);
  const paint = () => {
    const style = getComputedStyle(input);
    mirror.style.font = style.font;
    mirror.style.padding = `${style.paddingTop} ${style.paddingRight} ${style.paddingBottom} ${style.paddingLeft}`;
    mirror.style.letterSpacing = style.letterSpacing;
    mirror.replaceChildren(...varNodes(input.value));
    mirror.scrollLeft = input.scrollLeft;
  };
  input.addEventListener("input", () => requestAnimationFrame(paint));
  input.addEventListener("scroll", paint);
  input.addEventListener("keyup", () => requestAnimationFrame(paint));
  input.addEventListener("click", () => requestAnimationFrame(paint));
  input.refreshVars = paint;
  input.varField = wrap;
  requestAnimationFrame(paint);
  return wrap;
}

function shown(node) {
  return node?.varField || node;
}

function mountEditor(host, key, language, value, onChange, readOnly = false) {
  if (editors[key]) editors[key].destroy();
  const dark = document.documentElement.dataset.bsTheme === "dark";
  host.replaceChildren();
  editors[key] = new EditorView({
    doc: value || "",
    parent: host,
    extensions: [
      basicSetup, langOf(language), dark ? oneDark : [], varHighlight,
      key === "gql" && gqlNames.length ? autocompletion({ override: [completeFromList(gqlNames)] }) : [],
      EditorView.editable.of(!readOnly),
      EditorView.updateListener.of((update) => {
        if (update.docChanged && onChange) onChange(update.state.doc.toString());
      }),
    ],
  });
}

function editorValue(key) {
  return editors[key] ? editors[key].state.doc.toString() : null;
}

function flush() {
  const tab = active();
  if (!tab) return;
  const raw = editorValue("raw");
  const query = editorValue("gql");
  const variables = editorValue("gqlvars");
  const pre = editorValue("pre");
  const test = editorValue("test");
  if (raw !== null) tab.request.body.raw = raw;
  if (query !== null) tab.request.body.graphql.query = query;
  if (variables !== null) tab.request.body.graphql.variables = variables;
  if (pre !== null) tab.request.scripts.prerequest = pre;
  if (test !== null) tab.request.scripts.test = test;
}

function kvTable(rows, onChange, extra) {
  const wrap = document.createElement("div");
  const table = document.createElement("table");
  table.className = "table table-sm align-middle";
  const head = document.createElement("tr");
  for (const title of ["", "Key", "Value", "Description", ""]) {
    const th = document.createElement("th");
    th.textContent = title;
    head.append(th);
  }
  table.append(head);
  const bulk = document.createElement("button");
  bulk.type = "button";
  bulk.className = "btn btn-sm btn-outline-secondary mb-2";
  bulk.textContent = "Bulk edit";
  const area = document.createElement("textarea");
  area.className = "form-control form-control-sm d-none mb-2";
  area.rows = 6;
  area.value = (rows || []).filter((row) => row.key || row.value).map((row) => `${row.enabled === false ? "//" : ""}${row.key}:${row.value}`).join("\n");
  bulk.addEventListener("click", () => {
    area.classList.toggle("d-none");
    if (area.classList.contains("d-none")) {
      rows.splice(0, rows.length, ...area.value.split("\n").map((line) => {
        const disabled = line.startsWith("//");
        const raw = disabled ? line.slice(2) : line;
        const cut = raw.indexOf(":");
        if (cut < 0 || !raw.slice(0, cut).trim()) return null;
        return { key: raw.slice(0, cut).trim(), value: raw.slice(cut + 1), enabled: !disabled, description: "" };
      }).filter(Boolean));
      onChange("structure");
    }
  });
  rows.forEach((row, index) => {
    const tr = document.createElement("tr");
    const enabled = document.createElement("input");
    enabled.type = "checkbox";
    enabled.checked = row.enabled !== false;
    enabled.addEventListener("change", () => { row.enabled = enabled.checked; onChange(); });
    const key = field(row.key, (value) => { row.key = value; onChange(); });
    const val = field(row.value, (value) => { row.value = value; onChange(); });
    const description = field(row.description, (value) => { row.description = value; onChange(); });
    description.placeholder = "Description";
    tr.append(cell(enabled), cell(key), cell(val), cell(description));
    if (extra) tr.append(cell(extra(row, onChange)));
    const del = document.createElement("button");
    del.className = "row-x";
    del.type = "button";
    del.title = "Delete";
    del.setAttribute("aria-label", "Delete");
    del.append(icon("trash"));
    del.addEventListener("click", () => { rows.splice(index, 1); onChange("structure"); });
    tr.append(cell(del));
    table.append(tr);
  });
  const add = document.createElement("button");
  add.className = "btn btn-sm btn-outline-secondary";
  add.type = "button";
  add.append(icon("plus-lg"), document.createTextNode("Add"));
  add.addEventListener("click", () => { rows.push({ key: "", value: "", enabled: true, type: "text" }); onChange("structure"); });
  wrap.append(bulk, area, table, add);
  return wrap;
}

function field(value, onInput) {
  const input = document.createElement("input");
  input.className = "form-control form-control-sm";
  input.value = value || "";
  input.addEventListener("input", () => onInput(input.value));
  highlightVars(input);
  suggestFor(input);
  return input;
}

function cell(node) {
  const td = document.createElement("td");
  td.append(shown(node));
  return td;
}

function paint() {
  renderEnv();
  renderTree();
  renderHistory();
  renderTabs();
  const tab = active();
  $("empty").classList.toggle("d-none", !!tab);
  $("stage").classList.toggle("d-none", !tab);
  if (!tab) {
    document.title = "Restman";
    if ($("status-path")) $("status-path").textContent = snapshot?.settings?.workspacePath || "";
    return;
  }
  $("protocol").value = tab.request.protocol || "http";
  $("method").value = tab.request.method || "GET";
  $("method").dataset.method = $("method").value;
  ensureParams(tab.request);
  $("url").value = tab.request.url || "";
  $("url").refreshVars?.();
  let resolved = document.getElementById("resolved");
  if (!resolved) {
    resolved = document.createElement("div");
    resolved.id = "resolved";
    resolved.className = "resolved";
    $("url").closest(".request-line").after(resolved);
  }
  resolved.textContent = resolvedText(tab.request.url);
  resolved.title = resolved.textContent;
  $("crumb").textContent = breadcrumb(tab);
  $("req-doc").value = tab.request.description || "";
  $("req-name").value = tab.request.name || "";
  $("req-name").refreshVars?.();
  setSaveState(tab.dirty ? "dirty" : "idle");
  const socket = tab.request.protocol === "websocket";
  $("method").classList.toggle("d-none", socket);
  document.querySelector('[data-panel="body"]').classList.toggle("d-none", socket);
  document.querySelector('[data-rpanel="socket"]').classList.toggle("d-none", !socket);
  paintParams();
  paintVariables();
  paintChrome(tab);
  paintAuth();
  paintHeaders();
  paintBody();
  paintScripts();
  paintSettings();
  paintResponse();
  refreshVarHighlights();
}

function renderEnv() {
  const select = $("env");
  const current = snapshot.activeEnvironmentId || "";
  select.replaceChildren(new Option("No environment", ""));
  for (const env of snapshot.environments || []) select.append(new Option(env.name, env.id));
  select.value = current;
}

function renderTree() {
  const filter = $("filter").value.toLowerCase();
  const root = $("collections-pane");
  root.replaceChildren();
  const walk = (nodes, depth) => {
    for (const node of nodes || []) {
      const hay = `${node.name} ${node.url || ""}`.toLowerCase();
      if (filter && node.kind === "request" && !hay.includes(filter)) continue;
      if (filter && node.kind !== "request" && !hay.includes(filter) && !(node.children || []).some((kid) => `${kid.name} ${kid.url || ""}`.toLowerCase().includes(filter))) {
        /* still show containers that have a matching descendant via recursion */
      }
      const row = document.createElement("div");
      row.className = "tree-item";
      const button = document.createElement("button");
      button.type = "button";
      button.className = `tree-row kind-${node.kind}` + (node.id === selectedId ? " active" : "");
      button.style.paddingLeft = `${8 + depth * 14}px`;
      if (node.kind !== "request") {
        const chevron = document.createElement("span");
        chevron.className = "tree-chev";
        chevron.append(icon(collapsed.has(node.id) ? "chevron-right" : "chevron-down"));
        chevron.addEventListener("click", (event) => {
          event.stopPropagation();
          if (collapsed.has(node.id)) collapsed.delete(node.id);
          else collapsed.add(node.id);
          saveCollapsed();
          paint();
        });
        button.append(chevron);
      }
      if (node.kind === "collection") button.append(icon("collection"));
      else if (node.kind === "folder") button.append(icon("folder2"));
      else {
        const method = document.createElement("span");
        method.className = `method ${node.protocol === "websocket" ? "WS" : node.method}`;
        method.textContent = node.protocol === "websocket" ? "WS" : node.method;
        button.append(method);
      }
      const label = document.createElement("span");
      label.textContent = node.name;
      button.append(label);
      button.addEventListener("click", () => {
        if (suppressClick) { suppressClick = false; return; }
        selectedId = node.id;
        if (node.kind === "request") guard(() => openNode(node));
        else paint();
      });
      button.addEventListener("contextmenu", (event) => openTreeMenu(event, node));
      if (node.kind === "request" || node.kind === "folder") {
        button.draggable = true;
        button.addEventListener("dragstart", (event) => {
          dragId = node.id;
          suppressClick = true;
          event.dataTransfer.effectAllowed = "move";
          event.dataTransfer.setData("text/plain", node.id);
        });
        button.addEventListener("dragend", () => {
          dragId = null;
          clearDropMarks();
          setTimeout(() => { suppressClick = false; }, 50);
        });
      }
      button.addEventListener("dragover", (event) => {
        if (!dragId || dragId === node.id) return;
        event.preventDefault();
        event.dataTransfer.dropEffect = "move";
        clearDropMarks();
        if (node.kind === "request") {
          const after = event.clientY > button.getBoundingClientRect().top + button.offsetHeight / 2;
          button.classList.add(after ? "drop-after" : "drop-before");
        } else button.classList.add("drop-into");
      });
      button.addEventListener("dragleave", (event) => {
        if (button.contains(event.relatedTarget)) return;
        button.classList.remove("drop-before", "drop-after", "drop-into");
      });
      button.addEventListener("drop", (event) => {
        if (!dragId || dragId === node.id) return;
        event.preventDefault();
        const id = dragId;
        const after = node.kind === "request" && event.clientY > button.getBoundingClientRect().top + button.offsetHeight / 2;
        dragId = null;
        clearDropMarks();
        guard(() => moveRequest(id, node, after));
      });
      row.append(button);
      if (node.kind !== "request") {
        const add = document.createElement("button");
        add.type = "button";
        add.className = "tree-add";
        add.append(icon("plus-lg"));
        add.title = "Add request";
        add.setAttribute("aria-label", `Add request to ${node.name}`);
        add.addEventListener("click", (event) => {
          event.stopPropagation();
          selectedId = node.id;
          guard(() => createRequest("http"));
        });
        row.append(add);
      }
      root.append(row);
      if (!collapsed.has(node.id) || filter) walk(node.children, depth + 1);
    }
  };
  walk(snapshot.tree, 0);
}

async function openNode(node) {
  if (node.kind !== "request") return;
  const existing = tabs.find((tab) => tab.request.id === node.id);
  if (existing) { activeId = existing.id; paint(); return; }
  const request = normalize(await rpc("GetRequest", { id: node.id }));
  const parent = findParent(snapshot.tree, node.id);
  tabs.push({
    id: crypto.randomUUID(), request,
    collectionId: collectionOf(snapshot.tree, node.id),
    folderId: parent && parent.kind === "folder" ? parent.id : "",
    response: null, wsAfter: 0,
  });
  activeId = tabs.at(-1).id;
  paint();
  scheduleTabs();
}

function findParent(nodes, id, parent = null) {
  for (const node of nodes || []) {
    if (node.id === id) return parent;
    const found = findParent(node.children, id, node);
    if (found) return found;
  }
  return null;
}

function collectionOf(nodes, id, collection = "") {
  for (const node of nodes || []) {
    const next = node.kind === "collection" ? node.id : collection;
    if (node.id === id) return next;
    const found = collectionOf(node.children, id, next);
    if (found) return found;
  }
  return "";
}

function renderHistory() {
  const root = $("history-pane");
  root.replaceChildren();
  const filter = $("filter").value.toLowerCase();
  let lastDay = "";
  for (const entry of snapshot.history || []) {
    if (filter && !`${entry.name} ${entry.url} ${entry.method}`.toLowerCase().includes(filter)) continue;
    const day = dayLabel(entry.sentAt);
    if (day !== lastDay) {
      lastDay = day;
      const heading = document.createElement("div");
      heading.className = "history-day";
      heading.textContent = day;
      root.append(heading);
    }
    const button = document.createElement("button");
    button.type = "button";
    button.className = "history-row";
    const method = document.createElement("span");
    method.className = `method ${entry.method || ""}`;
    method.textContent = entry.method || "";
    const url = document.createElement("span");
    url.className = "history-url";
    url.textContent = `${entry.status || ""} ${entry.url || ""}`.trim();
    button.append(icon("clock-history"), method, url);
    button.addEventListener("contextmenu", (event) => {
      showContext(event, [
        { icon: "trash", label: "Delete", danger: true, run: () => guard(async () => { await rpc("DeleteHistory", { id: entry.id }); snapshot = await rpc("Bootstrap"); paint(); }) },
        { icon: "save", label: "Save as request", run: () => button.click() },
        { icon: "trash", label: "Clear history", danger: true, run: () => guard(clearHistory) },
      ]);
    });
    button.addEventListener("click", () => guard(async () => {
      if (entry.requestId && findById(snapshot.tree, entry.requestId)) {
        await openNode({ id: entry.requestId, kind: "request" });
        return;
      }
      const request = normalize(await rpc("HistoryRequest", { id: entry.id }));
      request.id = crypto.randomUUID();
      tabs.push({ id: crypto.randomUUID(), request, collectionId: "", folderId: "", response: null, wsAfter: 0 });
      activeId = tabs.at(-1).id;
      paint();
    }));
    root.append(button);
  }
}

function renderTabs() {
  const root = $("tabs");
  root.replaceChildren();
  for (const tab of tabs) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "req-tab" + (tab.id === activeId ? " active" : "");
    button.draggable = true;
    button.addEventListener("dragstart", (event) => { event.dataTransfer.setData("text/tab", tab.id); });
    button.addEventListener("dragover", (event) => event.preventDefault());
    button.addEventListener("drop", (event) => {
      event.preventDefault();
      const id = event.dataTransfer.getData("text/tab");
      const from = tabs.findIndex((item) => item.id === id);
      const to = tabs.findIndex((item) => item.id === tab.id);
      if (from < 0 || to < 0 || from === to) return;
      const [moved] = tabs.splice(from, 1);
      tabs.splice(to, 0, moved);
      paint();
    });
    button.addEventListener("auxclick", (event) => {
      if (event.button !== 1) return;
      event.preventDefault();
      closeTab(tab.id);
    });
    if (tab.dirty) {
      const dot = document.createElement("span");
      dot.className = "dirty-dot";
      button.append(dot);
    }
    if (tab.request.protocol === "websocket") button.append(icon("broadcast"));
    else {
      const method = document.createElement("span");
      method.className = `tab-method method ${tab.request.method || "GET"}`;
      method.textContent = tab.request.method || "GET";
      button.append(method);
    }
    const label = document.createElement("span");
    label.className = "tab-label";
    label.textContent = tab.request.name || "Untitled";
    button.append(label);
    button.addEventListener("click", () => { flush(); activeId = tab.id; paint(); });
    const close = document.createElement("span");
    close.className = "tab-x";
    close.title = "Close";
    close.append(icon("x-lg"));
    close.addEventListener("click", (event) => {
      event.stopPropagation();
      closeTab(tab.id);
      scheduleTabs();
    });
    button.append(close);
    root.append(button);
  }
}

function closeTab(id) {
  const tab = tabs.find((item) => item.id === id);
  if (tab?.dirty && !window.confirm(`Close ${tab.request.name || "Untitled"} without saving?`)) return;
  tabs = tabs.filter((item) => item.id !== id);
  if (activeId === id) activeId = tabs.at(-1)?.id || null;
  paint();
  scheduleTabs();
}

function paintParams() {
  const req = active().request;
  const onChange = (mode) => {
    writeQuery(req);
    $("url").value = req.url || "";
    $("url").refreshVars?.();
    const resolved = document.getElementById("resolved");
    if (resolved) resolved.textContent = resolvedText(req.url);
    mark();
    if (mode) paintParams();
  };
  $("panel-params").replaceChildren(kvTable(req.params, onChange));
}
function paintVariables() {
  $("panel-variables").replaceChildren(kvTable(active().request.variables, (mode) => { mark(); refreshVarHighlights(); if (mode) paintVariables(); }));
}
function paintChrome(tab) {
  const name = tab?.request?.name || "Restman";
  document.title = tab ? `${name} — Restman` : "Restman";
  rpc("SetTitle", { title: document.title }).catch(() => {});
  const collection = findById(snapshot?.tree, tab?.collectionId);
  $("status-path").textContent = collection ? `${collection.name} · ${snapshot?.settings?.workspacePath || ""}` : (snapshot?.settings?.workspacePath || "");
  $("cancel").classList.toggle("d-none", !tab?.sending);
  $("send").classList.toggle("busy", !!tab?.sending);
  $("send-label").textContent = tab?.sending ? "Sending" : "Send";
}
function paintHeaders() {
  const wrap = document.createElement("div");
  wrap.append(kvTable(active().request.headers, (mode) => { mark(); if (mode) paintHeaders(); }));
  const hidden = document.createElement("div");
  hidden.className = "section-label";
  hidden.textContent = "Hidden headers";
  wrap.append(hidden, tableOf(hiddenHeaderRows(), ["key", "value"]));
  $("panel-headers").replaceChildren(wrap);
}

function hiddenHeaderRows() {
  const req = active().request;
  let host = "";
  try { host = new URL(resolvedText(req.url).replace(/:([A-Za-z_][\w-]*)/g, "x")).host; } catch { host = ""; }
  const rows = [{ key: "Host", value: host }, { key: "User-Agent", value: "Restman" }];
  if (req.body?.mode === "raw") rows.push({ key: "Content-Type", value: req.body.language === "json" ? "application/json" : "text/plain" });
  if (req.body?.mode === "urlencoded") rows.push({ key: "Content-Type", value: "application/x-www-form-urlencoded" });
  if (req.body?.mode === "graphql") rows.push({ key: "Content-Type", value: "application/json" });
  const auth = authPreview(req.auth);
  if (auth.startsWith("Authorization:")) rows.push({ key: "Authorization", value: auth.slice("Authorization:".length).trim() });
  return rows.filter((row) => row.value);
}

function authPreview(auth) {
  if (!auth) return "";
  if ((auth.type === "basic" || auth.type === "digest" || auth.type === "ntlm") && auth.username) return `Authorization: ${auth.type} ${auth.username}`;
  if (auth.type === "bearer" && auth.token) return `Authorization: Bearer ${auth.token}`;
  if (auth.type === "apikey" && auth.key) return `${auth.in === "query" ? "Query" : "Header"} ${auth.key}: ${auth.value || ""}`;
  if (auth.type === "awsv4") return "Authorization: AWS4-HMAC-SHA256";
  if (auth.type === "oauth1") return "Authorization: OAuth";
  if (auth.type === "oauth2" && auth.oauth2?.accessToken) return `Authorization: ${auth.oauth2.tokenType || "Bearer"} ${auth.oauth2.accessToken}`;
  return "";
}

function paintAuth() {
  const auth = active().request.auth;
  const root = $("panel-auth");
  root.replaceChildren();
  const type = document.createElement("select");
  type.className = "form-select form-select-sm mb-2";
  for (const value of ["inherit", "none", "basic", "bearer", "digest", "ntlm", "apikey", "oauth2", "oauth1", "awsv4"]) type.append(new Option(value, value));
  type.value = auth.type || "inherit";
  type.addEventListener("change", () => { auth.type = type.value; mark(); paintAuth(); });
  root.append(type);
  const add = (label, key) => {
    const input = field(auth[key], (value) => { auth[key] = value; mark(); });
    input.placeholder = label;
    input.varField.classList.add("mb-2");
    root.append(shown(input));
  };
  if (auth.type === "basic" || auth.type === "digest" || auth.type === "ntlm") { add("Username", "username"); add("Password", "password"); }
  const preview = document.createElement("div");
  preview.className = "section-label";
  preview.textContent = authPreview(auth);
  if (preview.textContent) root.append(preview);
  if (auth.type === "bearer") add("Token", "token");
  if (auth.type === "apikey") {
    add("Key", "key"); add("Value", "value");
    const where = document.createElement("select");
    where.className = "form-select form-select-sm";
    where.append(new Option("header", "header"), new Option("query", "query"));
    where.value = auth.in || "header";
    where.addEventListener("change", () => { auth.in = where.value; mark(); });
    root.append(where);
  }
  if (auth.type === "oauth2") {
    auth.oauth2 ||= { grantType: "client_credentials", tokenUrl: "", clientId: "", clientSecret: "" };
    const oauth = auth.oauth2;
    const grant = document.createElement("select");
    grant.className = "form-select form-select-sm mb-2";
    grant.append(new Option("client_credentials", "client_credentials"), new Option("authorization_code", "authorization_code"), new Option("password", "password"), new Option("implicit", "implicit"), new Option("device_code", "device_code"));
    grant.value = oauth.grantType || "client_credentials";
    grant.addEventListener("change", () => { oauth.grantType = grant.value; mark(); paintAuth(); });
    root.append(grant);
    for (const [label, key] of [["Auth URL", "authUrl"], ["Token URL", "tokenUrl"], ["Client ID", "clientId"], ["Client secret", "clientSecret"], ["Username", "username"], ["Password", "password"], ["Scopes", "scopes"], ["Redirect URL", "redirectUrl"], ["Access token", "accessToken"]]) {
      const input = field(oauth[key], (value) => { oauth[key] = value; mark(); });
      input.placeholder = label;
      input.varField.classList.add("mb-2");
      root.append(shown(input));
    }
    const button = document.createElement("button");
    button.className = "btn btn-sm btn-primary";
    button.type = "button";
    button.append(icon("key"), document.createTextNode("Get token"));
    button.addEventListener("click", () => guard(async () => {
      active().request.auth = await rpc("Authorize", auth);
      mark();
      paintAuth();
    }));
    const pkce = document.createElement("label");
    pkce.className = "form-check mb-2";
    const box = document.createElement("input");
    box.type = "checkbox";
    box.className = "form-check-input";
    box.checked = !!oauth.usePkce;
    box.addEventListener("change", () => { oauth.usePkce = box.checked; mark(); });
    pkce.append(box, document.createTextNode(" Use PKCE"));
    root.append(pkce, button);
  }
  if (auth.type === "awsv4") {
    auth.aws ||= { accessKey: "", secretKey: "", region: "", service: "" };
    for (const [label, key] of [["Access key", "accessKey"], ["Secret key", "secretKey"], ["Region", "region"], ["Service", "service"], ["Session token", "sessionToken"]]) {
      const input = field(auth.aws[key], (value) => { auth.aws[key] = value; mark(); });
      input.placeholder = label;
      input.varField.classList.add("mb-2");
      root.append(shown(input));
    }
  }
  if (auth.type === "oauth1") {
    auth.oauth1 ||= { consumerKey: "", consumerSecret: "", token: "", tokenSecret: "" };
    for (const [label, key] of [["Consumer key", "consumerKey"], ["Consumer secret", "consumerSecret"], ["Token", "token"], ["Token secret", "tokenSecret"], ["Realm", "realm"]]) {
      const input = field(auth.oauth1[key], (value) => { auth.oauth1[key] = value; mark(); });
      input.placeholder = label;
      input.varField.classList.add("mb-2");
      root.append(shown(input));
    }
  }
}

const bodyModes = [
  ["none", "none", "slash-circle"],
  ["formdata", "form-data", "ui-checks"],
  ["urlencoded", "x-www-form-urlencoded", "link-45deg"],
  ["raw", "raw", "code-slash"],
  ["binary", "binary", "file-binary"],
  ["graphql", "GraphQL", "diagram-3"],
];

function paintBody() {
  const tab = active();
  const body = tab.request.body;
  body.graphql ||= { query: "", variables: "", operationName: "" };
  const root = $("panel-body");
  root.replaceChildren();
  const bar = document.createElement("div");
  bar.className = "body-bar";
  for (const [value, label, glyph] of bodyModes) {
    const item = document.createElement("label");
    const radio = document.createElement("input");
    radio.type = "radio";
    radio.name = "body-mode";
    radio.value = value;
    radio.checked = (body.mode || "none") === value;
    radio.addEventListener("change", () => {
      body.mode = value;
      if (value === "graphql" && tab.request.method === "GET") tab.request.method = "POST";
      mark();
      paintBody();
      $("method").value = tab.request.method;
      $("method").dataset.method = tab.request.method;
    });
    item.append(radio, icon(glyph), document.createTextNode(label));
    bar.append(item);
  }
  const tools = document.createElement("div");
  tools.className = "body-tools";
  if (body.mode === "raw") tools.append(rawLanguage(body));
  if (body.mode === "formdata" || body.mode === "urlencoded") tools.append(bulkButton(tab));
  bar.append(tools);
  root.append(bar);
  const fill = document.createElement("div");
  fill.className = "body-fill";
  root.append(fill);
  if (body.mode === "none" || !body.mode) {
    fill.append(hint("This request does not have a body."));
    return;
  }
  if (body.mode === "raw") {
    fill.append(hint(rawHint(body.language || "json")));
    const host = document.createElement("div");
    host.className = "cm-host grow";
    fill.append(host);
    mountEditor(host, "raw", editorLang(body.language), body.raw, (value) => { body.raw = value; mark(); });
    return;
  }
  if (body.mode === "urlencoded" || body.mode === "formdata") {
    const rows = body.mode === "formdata" ? body.formdata : body.urlencoded;
    fill.append(hint(body.mode === "formdata"
      ? "Sends text and files as multipart/form-data."
      : "Sends the fields as application/x-www-form-urlencoded."));
    fill.append(tab.bodyBulk ? bulkEditor(rows, body.mode === "formdata") : bodyGrid(rows, body.mode === "formdata"));
    return;
  }
  if (body.mode === "binary") {
    fill.append(hint("Sends the file as application/octet-stream."));
    fill.append(filePicker(body.filePath, (path) => { body.filePath = path; mark(); paintBody(); }));
    return;
  }
  fill.append(graphqlEditor(body));
}

function rawLanguage(body) {
  const wrap = document.createElement("div");
  wrap.className = "raw-tools";
  const lang = document.createElement("select");
  for (const [value, label] of [["json", "JSON"], ["text", "Text"], ["javascript", "JavaScript"], ["html", "HTML"], ["xml", "XML"]]) {
    lang.append(new Option(label, value));
  }
  lang.value = body.language || "json";
  lang.addEventListener("change", () => { body.language = lang.value; mark(); paintBody(); });
  wrap.append(lang);
  if ((body.language || "json") === "json") {
    const pretty = document.createElement("button");
    pretty.type = "button";
    pretty.append(icon("magic"), document.createTextNode("Beautify"));
    pretty.addEventListener("click", () => {
      try {
        body.raw = JSON.stringify(JSON.parse(body.raw || ""), null, 2);
        mark();
        paintBody();
      } catch (error) {
        toast(error);
      }
    });
    wrap.append(pretty);
  }
  return wrap;
}

function rawHint(language) {
  const types = { json: "application/json", text: "text/plain", javascript: "application/javascript", html: "text/html", xml: "application/xml" };
  return `Content-Type: ${types[language] || "text/plain"}`;
}

function editorLang(language) {
  if (!language || language === "text") return "text";
  if (language === "javascript") return "javascript";
  return language;
}

function bulkButton(tab) {
  const button = document.createElement("button");
  button.type = "button";
  button.append(icon(tab.bodyBulk ? "input-cursor-text" : "list-ul"), document.createTextNode(tab.bodyBulk ? "Key-Value Edit" : "Bulk Edit"));
  button.addEventListener("click", () => { tab.bodyBulk = !tab.bodyBulk; paintBody(); });
  return button;
}

function ensureBlank(rows) {
  const last = rows[rows.length - 1];
  if (!last || last.key || last.value || last.description || last.filePath) {
    rows.push({ key: "", value: "", description: "", enabled: true, type: "text", filePath: "" });
  }
}

function bodyGrid(rows, files) {
  ensureBlank(rows);
  const scroll = document.createElement("div");
  scroll.className = "body-scroll";
  const table = document.createElement("table");
  table.className = "body-table";
  const head = document.createElement("tr");
  const all = document.createElement("input");
  all.type = "checkbox";
  all.checked = rows.some((row) => row.enabled !== false && (row.key || row.value));
  all.addEventListener("change", () => {
    for (const row of rows) if (row.key || row.value || row.filePath) row.enabled = all.checked;
    mark();
    paintBody();
  });
  for (const title of ["", "Key", "Value", "Description", ""]) {
    const th = document.createElement("th");
    if (title === "" && head.children.length === 0) th.append(all);
    else th.textContent = title;
    head.append(th);
  }
  table.append(head);
  rows.forEach((_, index) => table.append(bodyRow(rows, files, index)));
  scroll.append(table);
  return scroll;
}

function bodyRow(rows, files, index) {
  const row = rows[index];
  const tr = document.createElement("tr");
  const enabled = document.createElement("input");
  enabled.type = "checkbox";
  enabled.checked = row.enabled !== false;
  enabled.addEventListener("change", () => { row.enabled = enabled.checked; mark(); });
  const keyCell = document.createElement("div");
  keyCell.className = "key-cell";
  const keyInput = lineField(row.key, "Key", (value) => { row.key = value; mark(); growRow(rowTable(keyInput), rows, index); });
  keyCell.append(shown(keyInput));
  if (files) {
    const type = document.createElement("select");
    type.append(new Option("Text", "text"), new Option("File", "file"));
    type.value = row.type || "text";
    type.addEventListener("change", () => { row.type = type.value; mark(); paintBody(); });
    keyCell.append(type);
  }
  let valueCell;
  if (row.type === "file" && files) {
    valueCell = fileButton(row.filePath, (path) => { row.filePath = path; row.value = path.split("/").pop(); mark(); paintBody(); });
  } else {
    valueCell = lineField(row.value, "Value", (value) => { row.value = value; mark(); growRow(rowTable(valueCell), rows, index); });
  }
  const description = lineField(row.description, "Description", (value) => { row.description = value; mark(); growRow(rowTable(description), rows, index); });
  const del = document.createElement("button");
  del.type = "button";
  del.className = "row-x";
  del.title = "Delete";
  del.setAttribute("aria-label", "Delete");
  del.append(icon("trash"));
  const blank = index === rows.length - 1 && !row.key && !row.value && !row.description && !row.filePath;
  del.disabled = blank;
  del.addEventListener("click", () => {
    if (del.disabled) return;
    rows.splice(index, 1);
    mark();
    paintBody();
  });
  tr.append(cell(enabled), cell(keyCell), cell(valueCell), cell(description), cell(del));
  return tr;
}

function rowTable(node) {
  return node.closest("table");
}

function growRow(table, rows, index) {
  if (!table || index !== rows.length - 1) return;
  const row = rows[index];
  if (!row.key && !row.value && !row.description && !row.filePath) return;
  const before = rows.length;
  ensureBlank(rows);
  if (rows.length === before) return;
  const current = table.rows[index + 1];
  const del = current?.querySelector(".row-x");
  if (del) del.disabled = false;
  const files = !!current?.querySelector(".key-cell select");
  table.append(bodyRow(rows, files, rows.length - 1));
  const all = table.querySelector("th input");
  if (all) all.checked = rows.some((item) => item.enabled !== false && (item.key || item.value || item.filePath));
}

function lineField(value, placeholder, onInput) {
  const input = document.createElement("input");
  input.type = "text";
  input.placeholder = placeholder;
  input.value = value || "";
  input.addEventListener("input", () => onInput(input.value));
  highlightVars(input);
  return input;
}

function bulkEditor(rows, files) {
  const area = document.createElement("textarea");
  area.className = "bulk-edit";
  area.spellcheck = false;
  area.placeholder = files ? "name:Ada\navatar:/path/to/file" : "key:value";
  area.value = rows.filter((row) => row.key || row.value).map((row) => `${row.enabled === false ? "//" : ""}${row.key}:${row.value || row.filePath || ""}`).join("\n");
  area.addEventListener("input", () => {
    const kept = area.value.split("\n").filter((line) => line.trim() !== "").map((line) => {
      const disabled = line.startsWith("//");
      const text = disabled ? line.slice(2) : line;
      const cut = text.indexOf(":");
      const key = cut === -1 ? text : text.slice(0, cut);
      const value = cut === -1 ? "" : text.slice(cut + 1);
      const previous = rows.find((row) => row.key === key);
      return {
        key, value: files && previous?.type === "file" ? previous.value : value,
        filePath: files && (previous?.type === "file" || value.startsWith("/")) ? (previous?.filePath || value) : "",
        description: previous?.description || "",
        enabled: !disabled,
        type: files && (previous?.type === "file" || value.startsWith("/")) ? "file" : "text",
      };
    });
    rows.splice(0, rows.length, ...kept);
    mark();
  });
  return area;
}

function fileButton(path, onPick) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "file-btn";
  button.append(icon("file-earmark"), document.createTextNode(path ? path.split("/").pop() : "Select File"));
  button.title = path || "Select File";
  button.addEventListener("click", () => guard(async () => {
    const picked = await rpc("PickFile");
    if (picked) onPick(picked);
  }));
  return button;
}

function filePicker(path, onPick) {
  const box = document.createElement("div");
  box.className = "binary-box";
  box.append(fileButton(path, onPick));
  if (path) {
    const name = document.createElement("div");
    name.className = "file-path";
    name.textContent = path;
    box.append(name);
  }
  return box;
}

function graphqlEditor(body) {
  const wrap = document.createElement("div");
  wrap.className = "gql-wrap";
  const op = lineField(body.graphql.operationName, "Operation name", (value) => { body.graphql.operationName = value; mark(); });
  op.classList.add("gql-op");
  op.varField.classList.add("gql-op-field");
  const split = document.createElement("div");
  split.className = "gql-split";
  const queryPane = document.createElement("div");
  const varsPane = document.createElement("div");
  queryPane.className = varsPane.className = "gql-pane";
  const queryLabel = document.createElement("div");
  queryLabel.className = "gql-label";
  queryLabel.append(icon("diagram-3"), document.createTextNode("Query"));
  const varsLabel = document.createElement("div");
  varsLabel.className = "gql-label";
  varsLabel.append(icon("braces"), document.createTextNode("GraphQL variables"));
  const query = document.createElement("div");
  const vars = document.createElement("div");
  query.className = vars.className = "cm-host grow";
  queryPane.append(queryLabel, query);
  varsPane.append(varsLabel, vars);
  split.append(queryPane, varsPane);
  const intro = document.createElement("button");
  intro.type = "button";
  intro.className = "btn btn-sm btn-outline-secondary mb-2";
  intro.append(icon("diagram-3"), document.createTextNode("Load schema"));
  intro.addEventListener("click", () => guard(loadSchema));
  wrap.append(shown(op), intro, split);
  mountEditor(query, "gql", "javascript", body.graphql.query, (value) => { body.graphql.query = value; mark(); });
  mountEditor(vars, "gqlvars", "json", body.graphql.variables, (value) => { body.graphql.variables = value; mark(); });
  return wrap;
}

function hint(text) {
  const node = document.createElement("div");
  node.className = "body-hint";
  node.textContent = text;
  return node;
}

function paintScripts() {
  const scripts = active().request.scripts;
  const root = $("panel-scripts");
  root.replaceChildren();
  const pre = document.createElement("div");
  const test = document.createElement("div");
  pre.className = test.className = "cm-host mb-2";
  const preLabel = document.createElement("div");
  preLabel.className = "section-label";
  preLabel.append(icon("lightning-charge"), document.createTextNode("Pre-request"));
  const testLabel = document.createElement("div");
  testLabel.className = "section-label";
  testLabel.append(icon("check2-circle"), document.createTextNode("Tests"));
  const snippets = document.createElement("div");
  snippets.className = "d-flex flex-wrap gap-1 mb-2";
  for (const [label, source] of [
    ["Status is 200", "pm.test(\"status\", function () {\n  pm.expect(pm.response.code).to.equal(200);\n});\n"],
    ["Body has id", "pm.test(\"id\", function () {\n  pm.expect(pm.response.json().id).to.be.ok;\n});\n"],
    ["Log the body", "console.log(pm.response.text());\n"],
  ]) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "btn btn-sm btn-outline-secondary";
    button.textContent = label;
    button.addEventListener("click", () => { scripts.test = `${scripts.test || ""}${source}`; mark(); paintScripts(); });
    snippets.append(button);
  }
  root.append(snippets, preLabel, pre, testLabel, test);
  mountEditor(pre, "pre", "javascript", scripts.prerequest, (value) => { scripts.prerequest = value; mark(); });
  mountEditor(test, "test", "javascript", scripts.test, (value) => { scripts.test = value; mark(); });
}

function paintSettings() {
  const tab = active();
  const options = tab.request.options;
  const root = $("panel-settings");
  root.replaceChildren();
  const form = document.createElement("div");
  const number = (label, key) => {
    const input = document.createElement("input");
    input.className = "form-control form-control-sm";
    input.type = "number";
    input.value = options[key] || "";
    input.addEventListener("input", () => { options[key] = Number(input.value) || 0; mark(); });
    form.append(labeled(label, input));
  };
  const choice = (label, key, values) => {
    const select = document.createElement("select");
    select.className = "form-select form-select-sm";
    for (const [value, text] of values) select.append(new Option(text, value));
    select.value = options[key] || "";
    select.addEventListener("change", () => { options[key] = select.value; mark(); });
    form.append(labeled(label, select));
  };
  number("Timeout seconds, 0 uses the global setting", "timeoutSeconds");
  choice("Follow redirects", "followRedirects", [["", "Use global setting"], ["yes", "Yes"], ["no", "No"]]);
  choice("Skip TLS verify", "insecure", [["", "Use global setting"], ["yes", "Yes"], ["no", "No"]]);
  choice("Cookie jar", "cookies", [["", "Use the jar"], ["no", "Do not send cookies"]]);
  choice("HTTP version", "httpVersion", [["", "Auto"], ["1.1", "HTTP/1.1"], ["2", "HTTP/2"]]);
  const proxy = document.createElement("input");
  proxy.className = "form-control form-control-sm";
  proxy.placeholder = "empty inherits, none disables, or an http URL";
  proxy.value = options.proxy || "";
  proxy.addEventListener("input", () => { options.proxy = proxy.value.trim(); mark(); });
  form.append(labeled("Proxy", proxy));
  const protocols = document.createElement("input");
  protocols.className = "form-control form-control-sm";
  protocols.placeholder = "comma separated";
  protocols.value = options.subprotocols || "";
  protocols.addEventListener("input", () => { options.subprotocols = protocols.value; mark(); });
  form.append(labeled("WebSocket subprotocols", protocols));
  number("WebSocket ping seconds", "pingSeconds");
  const examples = document.createElement("div");
  const heading = document.createElement("div");
  heading.className = "section-label";
  heading.append(icon("bookmark"), document.createTextNode("Saved examples"));
  examples.append(heading);
  if (!tab.request.examples.length) {
    const empty = document.createElement("p");
    empty.className = "text-secondary";
    empty.textContent = "Save the current response as an example from the response pane.";
    examples.append(empty);
  }
  for (const example of tab.request.examples) {
    const row = document.createElement("div");
    row.className = "d-flex gap-2 align-items-center mb-1";
    const load = document.createElement("button");
    load.type = "button";
    load.className = "btn btn-sm btn-outline-secondary";
    load.textContent = `${example.status || ""} ${example.name}`.trim();
    load.addEventListener("click", () => {
      tab.response = { status: example.status, statusText: example.statusText || "", headers: example.headers || [], cookies: [], body: example.body || "", size: (example.body || "").length, timing: {}, tests: [] };
      paintResponse();
    });
    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = "icon-btn";
    remove.title = "Delete example";
    remove.append(icon("trash"));
    remove.addEventListener("click", () => {
      tab.request.examples = tab.request.examples.filter((item) => item.id !== example.id);
      mark();
      paintSettings();
    });
    row.append(load, remove);
    examples.append(row);
  }
  root.append(form, examples);
}

function labeled(text, control) {
  const label = document.createElement("label");
  label.className = "d-block mb-2";
  const title = document.createElement("div");
  title.className = "section-label";
  title.textContent = text;
  label.append(title, control);
  return label;
}

function paintResponse() {
  const tab = active();
  const response = tab.response;
  $("status").textContent = response ? (response.error && !response.status ? response.error : `${response.status} ${response.statusText || ""}`) : "Ready";
  const code = response?.status || 0;
  $("status").className = code >= 200 && code < 300 ? "ok" : code ? "err" : "";
  const timing = response?.timing;
  $("timing").replaceChildren();
  if (timing) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "icon-btn";
    button.textContent = `${timing.totalMs} ms`;
    button.title = `DNS ${timing.dnsMs || 0} ms · Connect ${timing.connectMs || 0} ms · TLS ${timing.tlsMs || 0} ms · TTFB ${timing.ttfbMs || 0} ms`;
    button.addEventListener("click", () => {
      const list = document.createElement("div");
      list.append(document.createTextNode(button.title));
      for (const hop of response.redirects || []) {
        const line = document.createElement("div");
        line.textContent = `${hop.status} ${hop.url}`;
        list.append(line);
      }
      if (!(response.redirects || []).length) {
        const line = document.createElement("div");
        line.textContent = "No redirects";
        list.append(line);
      }
      openModal("Timing", list);
    });
    $("timing").append(button);
  }
  $("size").textContent = response ? `${response.size} B` : "";
  let body = response?.body || "";
  const query = ($("body-find")?.value || "").toLowerCase();
  if (bodyView === "pretty" && body) {
    try { body = JSON.stringify(JSON.parse(body), null, 2); } catch { /* keep raw */ }
  }
  $("response-preview").classList.toggle("d-none", bodyView !== "preview");
  $("response-editor").classList.toggle("d-none", bodyView === "preview");
  if (bodyView === "preview") $("response-preview").srcdoc = response?.body || "";
  const host = $("response-editor");
  if (query && body && !body.toLowerCase().includes(query)) body = "";
  if (!body || bodyView === "preview") {
    if (editors.response) editors.response.destroy();
    delete editors.response;
    host.replaceChildren();
  } else {
    mountEditor(host, "response", "json", body, null, true);
  }
  $("rpanel-headers").replaceChildren(tableOf(response?.headers || [], ["key", "value"]));
  $("rpanel-cookies").replaceChildren(tableOf(response?.cookies || [], ["name", "value", "domain", "path"]));
  const tests = document.createElement("div");
  for (const item of response?.tests || []) {
    const row = document.createElement("div");
    row.className = item.pass ? "text-success" : "text-danger";
    const name = document.createElement("strong");
    name.textContent = `${item.pass ? "PASS" : "FAIL"} ${item.name}`;
    row.append(name);
    if (item.error) {
      const error = document.createElement("div");
      error.textContent = item.error;
      row.append(error);
    }
    tests.append(row);
  }
  for (const line of response?.console || []) {
    const row = document.createElement("div");
    row.textContent = `console ${line}`;
    tests.append(row);
  }
  if (response?.scriptError) {
    const row = document.createElement("div");
    row.className = "text-danger";
    row.textContent = response.scriptError;
    tests.append(row);
  }
  $("rpanel-tests").replaceChildren(tests);
  const log = $("ws-log");
  log.replaceChildren();
  for (const message of tab.wsLog || []) {
    const line = document.createElement("div");
    line.className = "ws-line";
    const dir = document.createElement("span");
    dir.className = "dir";
    dir.textContent = message.direction;
    const when = document.createElement("span");
    when.textContent = message.time || "";
    const data = document.createElement("span");
    data.textContent = `${message.binary ? "[binary] " : ""}${message.data}`;
    line.append(dir, when, data);
    log.append(line);
  }
}

function tableOf(rows, keys) {
  const table = document.createElement("table");
  table.className = "table table-sm";
  const head = document.createElement("tr");
  for (const key of keys) {
    const th = document.createElement("th");
    th.textContent = key;
    head.append(th);
  }
  if (keys[0] === "name" && keys.includes("domain")) {
    const th = document.createElement("th");
    th.textContent = "";
    head.append(th);
  }
  table.append(head);
  for (const row of rows) {
    const tr = document.createElement("tr");
    for (const key of keys) {
      const td = document.createElement("td");
      td.textContent = row[key] || "";
      tr.append(td);
    }
    if (row.domain && row.name) {
      const td = document.createElement("td");
      const save = document.createElement("button");
      save.type = "button";
      save.className = "btn btn-sm btn-outline-secondary";
      save.textContent = "Save";
      save.addEventListener("click", () => guard(async () => {
        await rpc("SaveCookie", row);
        note("Cookie saved", true);
      }));
      td.append(save);
      tr.append(td);
    }
    table.append(tr);
  }
  return table;
}

function mark() {
  const tab = active();
  if (!tab) return;
  tab.dirty = true;
  setSaveState("dirty");
}

function setSaveState(state) {
  const button = $("save");
  const saved = state === "saved";
  button.replaceChildren(icon(saved ? "check-lg" : "floppy"));
  if (state === "dirty") button.append(document.createTextNode("Save"));
  button.title = saved ? "Saved" : state === "dirty" ? "Save" : "Save";
  button.setAttribute("aria-label", button.title);
  button.classList.toggle("dirty", state === "dirty");
}

function showPanel(name) {
  for (const id of ["params", "variables", "auth", "headers", "body", "scripts", "settings"]) {
    $("panel-" + id).classList.toggle("d-none", id !== name);
  }
  for (const button of $("subtabs").querySelectorAll("button")) button.classList.toggle("active", button.dataset.panel === name);
}

function showResponse(name) {
  for (const id of ["body", "headers", "cookies", "tests", "socket"]) {
    $("rpanel-" + id).classList.toggle("d-none", id !== name);
  }
  for (const button of $("response-tabs").querySelectorAll("button")) button.classList.toggle("active", button.dataset.rpanel === name);
}

function parentForNew() {
  const node = findById(snapshot.tree, selectedId);
  if (!node) return { parentId: "", collectionId: "", folderId: "" };
  if (node.kind === "request") {
    const parent = findParent(snapshot.tree, node.id);
    return { parentId: parent?.id || "", collectionId: collectionOf(snapshot.tree, node.id), folderId: parent?.kind === "folder" ? parent.id : "" };
  }
  return { parentId: node.id, collectionId: node.kind === "collection" ? node.id : collectionOf(snapshot.tree, node.id), folderId: node.kind === "folder" ? node.id : "" };
}

function findById(nodes, id) {
  for (const node of nodes || []) {
    if (node.id === id) return node;
    const found = findById(node.children, id);
    if (found) return found;
  }
  return null;
}

async function createRequest(protocol) {
  const place = parentForNew();
  const request = normalize(await rpc("CreateRequest", { parentId: place.parentId, name: protocol === "websocket" ? "Socket" : "Untitled", protocol }));
  if (!place.collectionId) {
    snapshot = await rpc("Bootstrap");
  }
  tabs.push({
    id: crypto.randomUUID(), request,
    collectionId: place.collectionId || collectionOf(snapshot.tree, request.id),
    folderId: place.folderId, response: null, wsAfter: 0,
  });
  activeId = tabs.at(-1).id;
  selectedId = request.id;
  snapshot = await rpc("Bootstrap");
  paint();
  const parent = findById(snapshot.tree, place.parentId) || findById(snapshot.tree, tabCollection(request.id));
  note(parent ? `Added to ${parent.name}` : "Request added", true);
}

function tabCollection(id) {
  return collectionOf(snapshot.tree, id);
}

async function saveActive() {
  flush();
  const tab = active();
  if (!tab) {
    note("Open a request to save");
    return;
  }
  tab.request.name = ($("req-name").value || tab.request.name || "Untitled").trim() || "Untitled";
  if (!findById(snapshot.tree, tab.request.id)) {
    const choice = await askSave(tab.request.name);
    if (!choice) return;
    tab.request.name = choice.name;
    tab.collectionId = choice.collectionId;
    tab.folderId = "";
  }
  snapshot = await rpc("SaveRequest", { parentId: tab.folderId || tab.collectionId, request: tab.request });
  tab.collectionId = collectionOf(snapshot.tree, tab.request.id) || tab.collectionId;
  tab.dirty = false;
  selectedId = tab.request.id;
  paint();
  scheduleTabs();
  setSaveState("saved");
  clearTimeout(saveActive.timer);
  saveActive.timer = setTimeout(() => {
    if (!active()?.dirty) setSaveState("idle");
  }, 1400);
  const collection = findById(snapshot.tree, tab.collectionId);
  note(collection ? `Saved to ${collection.name}` : "Saved", true);
}

async function sendActive() {
  flush();
  const tab = active();
  if (!tab) return;
  tab.sending = true;
  paintChrome(tab);
  try {
  if (tab.request.protocol === "websocket") {
    const options = tab.request.options || {};
    const protocols = (options.subprotocols || "").split(",").map((item) => item.trim()).filter(Boolean);
    await rpc("WSConnect", { tabId: tab.id, url: tab.request.url, headers: tab.request.headers, protocols, pingSeconds: options.pingSeconds || 0 });
    tab.wsLog = [];
    tab.wsAfter = 0;
    showResponse("socket");
    paint();
    return;
  }
  tab.response = await rpc("Send", { tabId: tab.id, collectionId: tab.collectionId, folderId: tab.folderId, request: tab.request });
  snapshot = await rpc("Bootstrap");
  showResponse("body");
  paint();
  } finally {
    tab.sending = false;
    paintChrome(tab);
  }
}

function scheduleTabs() {
  clearTimeout(scheduleTabs.timer);
  scheduleTabs.timer = setTimeout(() => {
    const payload = tabs.map((tab) => ({ id: tab.id, request: tab.request, collectionId: tab.collectionId, folderId: tab.folderId }));
    rpc("SaveTabs", { tabs: JSON.stringify(payload) }).catch(() => {});
  }, 300);
}

function closeSnippet() {
  if (!snippetView) return;
  snippetView.destroy();
  snippetView = null;
}

function openModal(title, node) {
  closeSnippet();
  const card = $("modal-card");
  card.replaceChildren();
  const bar = document.createElement("div");
  bar.className = "modal-bar";
  const heading = document.createElement("h2");
  heading.textContent = title;
  const close = document.createElement("button");
  close.className = "icon-btn";
  close.type = "button";
  close.title = "Close";
  close.setAttribute("aria-label", "Close");
  close.append(icon("x-lg"));
  close.addEventListener("click", () => {
    closeSnippet();
    $("modal").classList.add("d-none");
  });
  bar.append(heading, close);
  card.append(bar, node);
  $("modal").classList.remove("d-none");
}

function askSave(name) {
  return new Promise((resolve) => {
    const collections = (snapshot.tree || []).filter((node) => node.kind === "collection");
    const form = document.createElement("form");
    const input = document.createElement("input");
    input.className = "form-control form-control-sm my-2";
    input.placeholder = "Request name";
    input.value = name || "Untitled";
    input.required = true;
    const select = document.createElement("select");
    select.className = "form-select form-select-sm mb-2";
    for (const collection of collections) select.append(new Option(collection.name, collection.id));
    const button = document.createElement("button");
    button.className = "btn btn-sm btn-primary";
    button.type = "submit";
    button.append(icon("floppy"), document.createTextNode("Save"));
    if (!collections.length) {
      const hint = document.createElement("p");
      hint.className = "text-secondary";
      hint.textContent = "Create a collection from the + menu, then save the request into it.";
      form.append(hint);
      button.disabled = true;
    }
    form.append(input, select, button);
    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      $("modal").classList.add("d-none");
      resolve(value);
    };
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      finish({ name: input.value.trim() || "Untitled", collectionId: select.value });
    });
    openModal("Save request", form);
    $("modal-card").querySelector("button").addEventListener("click", () => finish(null));
    input.focus();
    input.select();
  });
}

function askName(title) {
  return new Promise((resolve) => {
    const form = document.createElement("form");
    const input = document.createElement("input");
    input.className = "form-control form-control-sm my-2";
    input.required = true;
    const button = document.createElement("button");
    button.className = "btn btn-sm btn-primary";
    button.append(icon("plus-lg"), document.createTextNode("Create"));
    form.append(input, button);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      $("modal").classList.add("d-none");
      resolve(input.value.trim());
    });
    openModal(title, form);
    input.focus();
  });
}

function settingsView() {
  const form = document.createElement("form");
  const settings = { ...snapshot.settings };
  const fields = [
    ["Timeout seconds", "timeoutSeconds", "number"],
    ["Proxy URL", "proxyUrl", "text"],
    ["Client cert file", "clientCertFile", "text"],
    ["Client key file", "clientKeyFile", "text"],
    ["Workspace path", "workspacePath", "text"],
  ];
  for (const [label, key, type] of fields) {
    const input = document.createElement("input");
    input.className = "form-control form-control-sm mb-2";
    input.placeholder = label;
    input.type = type;
    input.value = settings[key] ?? "";
    input.addEventListener("input", () => { settings[key] = type === "number" ? Number(input.value) : input.value; });
    form.append(input);
  }
  for (const [label, key] of [["Follow redirects", "followRedirects"], ["Skip TLS verify", "insecureSkipVerify"]]) {
    const labelEl = document.createElement("label");
    labelEl.className = "form-check mb-2";
    const input = document.createElement("input");
    input.type = "checkbox";
    input.className = "form-check-input";
    input.checked = !!settings[key];
    input.addEventListener("change", () => { settings[key] = input.checked; });
    labelEl.append(input, document.createTextNode(" " + label));
    form.append(labelEl);
  }
  const save = document.createElement("button");
  save.className = "btn btn-sm btn-primary";
  save.append(icon("floppy"), document.createTextNode("Save settings"));
  save.type = "submit";
  form.append(save);
  form.addEventListener("submit", (event) => guard(async () => {
    event.preventDefault();
    const path = settings.workspacePath;
    delete settings.workspacePath;
    snapshot = await rpc("SaveSettings", settings);
    if (path && path !== snapshot.workspacePath) snapshot = await rpc("OpenWorkspace", { path });
    applyTheme(snapshot.settings.theme);
    $("modal").classList.add("d-none");
    paint();
  }));
  return form;
}

function globalsView() {
  const vars = (snapshot.globals || []).map((item) => ({ ...item }));
  const wrap = document.createElement("div");
  const save = document.createElement("button");
  save.className = "btn btn-sm btn-primary mt-2";
  save.type = "button";
  save.append(icon("floppy"), document.createTextNode("Save globals"));
  save.addEventListener("click", () => guard(async () => {
    snapshot = await rpc("SaveGlobals", { variables: vars });
    paint();
  }));
  const redraw = () => { wrap.replaceChildren(kvTable(vars, (mode) => { if (mode) redraw(); }), save); };
  redraw();
  return wrap;
}

function envView() {
  const wrap = document.createElement("div");
  const select = document.createElement("select");
  select.className = "form-select form-select-sm mb-2";
  let vars = [];
  const fill = () => {
    select.replaceChildren();
    for (const env of snapshot.environments || []) select.append(new Option(env.name, env.id));
    const env = (snapshot.environments || []).find((item) => item.id === select.value);
    vars = (env?.variables || []).map((item) => ({ ...item }));
    draw();
  };
  const draw = () => {
    const actions = document.createElement("div");
    actions.className = "d-flex gap-2 mb-2";
    const save = button("Save", () => guard(async () => {
      const env = (snapshot.environments || []).find((item) => item.id === select.value);
      if (!env) return;
      snapshot = await rpc("SaveEnvironment", { ...env, variables: vars });
      fill();
    }), "floppy");
    const add = button("New", () => guard(async () => {
      const name = window.prompt("Environment name");
      if (!name) return;
      snapshot = await rpc("SaveEnvironment", { name, variables: [] });
      fill();
    }), "plus-lg");
    const remove = button("Delete", () => guard(async () => {
      if (!select.value) return;
      snapshot = await rpc("DeleteEnvironment", { id: select.value });
      fill();
    }), "trash");
    const imp = button("Import", () => { importKind = "environment"; $("file").click(); }, "box-arrow-in-down");
    const exp = button("Export", () => guard(async () => download(`${select.value}.environment.json`, await rpc("ExportEnvironment", { id: select.value }))), "box-arrow-up");
    actions.append(save, add, remove, imp, exp);
    wrap.replaceChildren(select, actions, kvTable(vars, (mode) => { if (mode) draw(); }));
  };
  select.addEventListener("change", fill);
  fill();
  return wrap;
}

function button(text, onClick, glyph) {
  const el = document.createElement("button");
  el.className = "btn btn-sm btn-outline-secondary";
  el.type = "button";
  if (glyph) el.append(icon(glyph));
  el.append(document.createTextNode(text));
  el.addEventListener("click", onClick);
  return el;
}

async function download(name, content) {
  const text = typeof content === "string" ? content : JSON.stringify(content, null, 2);
  try {
    return await rpc("SaveFile", { name, content: text });
  } catch (error) {
    if (!/desktop window/i.test(error.message || "")) throw error;
  }
  const blob = new Blob([text], { type: "application/json" });
  const link = document.createElement("a");
  link.href = URL.createObjectURL(blob);
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(link.href), 1500);
  return name;
}

function cookiesView() {
  const wrap = document.createElement("div");
  guard(async () => {
    const cookies = await rpc("Cookies");
    wrap.replaceChildren();
    const form = document.createElement("form");
    form.className = "d-flex gap-2 mb-2";
    const domain = document.createElement("input");
    const name = document.createElement("input");
    const value = document.createElement("input");
    for (const input of [domain, name, value]) input.className = "form-control form-control-sm";
    domain.placeholder = "Domain";
    name.placeholder = "Name";
    value.placeholder = "Value";
    const add = document.createElement("button");
    add.className = "btn btn-sm btn-primary";
    add.textContent = "Add";
    add.type = "submit";
    form.append(domain, name, value, add);
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      guard(async () => {
        await rpc("SaveCookie", { domain: domain.value, path: "/", name: name.value, value: value.value });
        openModal("Cookies", cookiesView());
      });
    });
    wrap.append(form);
    const groups = new Map();
    for (const cookie of cookies) {
      if (!groups.has(cookie.domain)) groups.set(cookie.domain, []);
      groups.get(cookie.domain).push(cookie);
    }
    for (const [group, items] of groups) {
      const heading = document.createElement("div");
      heading.className = "section-label";
      heading.textContent = group || "Cookies";
      wrap.append(heading);
      for (const cookie of items) {
      const row = document.createElement("div");
      row.className = "d-flex gap-2 mb-1";
      const label = document.createElement("span");
      label.textContent = `${cookie.domain}${cookie.path} ${cookie.name}=${cookie.value}`;
      const del = button("Delete", () => guard(async () => {
        await rpc("DeleteCookie", cookie);
        row.remove();
      }), "trash");
      row.append(label, del);
      wrap.append(row);
      }
    }
  });
  return wrap;
}

function bind() {
  $("env").addEventListener("change", () => guard(async () => {
    snapshot = await rpc("SetActiveEnvironment", { id: $("env").value });
    refreshVarHighlights();
  }));
  $("theme").addEventListener("click", () => guard(async () => {
    const order = ["dark", "light", "system"];
    const theme = order[(order.indexOf(snapshot.settings.theme || "dark") + 1) % order.length];
    snapshot = await rpc("SaveSettings", { ...snapshot.settings, theme });
    applyTheme(theme);
    paint();
  }));
  $("show-collections").addEventListener("click", () => setSidebar("collections"));
  $("show-history").addEventListener("click", () => setSidebar("history"));
  $("filter").addEventListener("input", () => { renderTree(); renderHistory(); });
  $("new-collection").addEventListener("click", () => guard(async () => {
    const name = await askName("Collection name");
    if (!name) return;
    snapshot = await rpc("CreateCollection", { name });
    paint();
  }));
  $("new-folder").addEventListener("click", () => guard(async () => {
    const place = parentForNew();
    if (!place.parentId) return toast(new Error("Select a collection or folder"));
    const name = await askName("Folder name");
    if (!name) return;
    snapshot = await rpc("CreateFolder", { parentId: place.parentId, name });
    paint();
  }));
  $("new-request").addEventListener("click", () => guard(() => createRequest("http")));
  $("empty-new").addEventListener("click", () => guard(() => createRequest("http")));
  $("new-socket").addEventListener("click", () => guard(() => createRequest("websocket")));
  $("import-collection").addEventListener("click", () => { importKind = "collection"; $("file").click(); });
  $("import-curl").addEventListener("click", () => importCurl());
  $("copy-curl").addEventListener("click", (event) => {
    event.stopPropagation();
    showContext(event, snippetLanguages.map(([value, label]) => ({
      icon: "clipboard",
      label,
      run: () => guard(() => copyLanguage(value, label)),
    })));
  });
  $("save-example").addEventListener("click", () => saveExample());
  $("history-pane").addEventListener("contextmenu", (event) => {
    showContext(event, [{ icon: "trash", label: "Clear history", run: () => guard(clearHistory) }]);
  });
  $("file").addEventListener("change", () => guard(async () => {
    const file = $("file").files[0];
    if (!file) return;
    const content = await file.text();
    snapshot = await rpc(importKind === "environment" ? "ImportEnvironment" : "ImportPostman", { content });
    $("file").value = "";
    paint();
  }));
  $("save").addEventListener("click", () => guard(saveActive));
  $("send").addEventListener("click", () => guard(sendActive));
  $("cancel").addEventListener("click", () => guard(() => rpc("Cancel", { tabId: activeId })));
  $("protocol").addEventListener("change", () => { active().request.protocol = $("protocol").value; mark(); paint(); });
  $("method").addEventListener("change", () => { active().request.method = $("method").value; $("method").dataset.method = $("method").value; mark(); });
  $("new-toggle").addEventListener("click", (event) => {
    event.stopPropagation();
    $("new-menu").classList.toggle("d-none");
  });
  document.addEventListener("click", () => { $("new-menu").classList.add("d-none"); hideContext(); });
  document.addEventListener("keydown", (event) => { if (event.key === "Escape") hideContext(); });
  $("collections-pane").addEventListener("scroll", hideContext);
  $("new-menu").addEventListener("click", () => $("new-menu").classList.add("d-none"));
  $("split").addEventListener("pointerdown", (event) => {
    event.preventDefault();
    const startY = event.clientY;
    const start = $("editor").getBoundingClientRect().height;
    const move = (e) => {
      const next = Math.max(160, start + (e.clientY - startY));
      $("editor").style.flex = `0 0 ${next}px`;
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  });
  highlightVars($("url"));
  highlightVars($("req-name"));
  highlightVars($("ws-input"));
  $("url").addEventListener("input", () => {
    const req = active().request;
    req.url = $("url").value;
    req.params = [...pathKeys(req.url).map((key) => ({ key, value: (req.params || []).find((row) => row.key === key && row.description === "path")?.value || "", enabled: true, description: "path" })), ...parseQuery(req.url)];
    const resolved = document.getElementById("resolved");
    if (resolved) { resolved.textContent = resolvedText(req.url); resolved.title = resolved.textContent; }
    mark();
    if (!$("panel-params").classList.contains("d-none")) paintParams();
  });
  suggestFor($("url"));
  $("req-doc").addEventListener("input", () => { active().request.description = $("req-doc").value; mark(); });
  $("req-name").addEventListener("input", () => {
    const tab = active();
    if (!tab) return;
    tab.request.name = $("req-name").value;
    mark();
    const label = document.querySelector(".req-tab.active .tab-label");
    if (label) label.textContent = tab.request.name || "Untitled";
  });
  document.querySelectorAll(".body-view button").forEach((button) => {
    button.addEventListener("click", () => {
      bodyView = button.dataset.view;
      document.querySelectorAll(".body-view button").forEach((item) => item.classList.toggle("active", item === button));
      paintResponse();
    });
  });
  $("body-find").addEventListener("input", paintResponse);
  $("save-body").addEventListener("click", () => guard(saveBody));
  $("copy-body").addEventListener("click", () => guard(async () => {
    const body = active()?.response?.body || "";
    try { await navigator.clipboard.writeText(body); note("Copied response", true); }
    catch { note("Could not copy"); }
  }));
  $("send-menu").addEventListener("click", (event) => {
    event.stopPropagation();
    showContext(event, [{ icon: "download", label: "Send and download", run: () => guard(async () => { await sendActive(); saveBody(); }) }]);
  });
  $("subtabs").addEventListener("click", (event) => {
    const button = event.target.closest("[data-panel]");
    if (button) showPanel(button.dataset.panel);
  });
  $("response-tabs").addEventListener("click", (event) => { if (event.target.dataset.rpanel) showResponse(event.target.dataset.rpanel); });
  $("settings").addEventListener("click", () => openModal("Settings", settingsView()));
  $("globals").addEventListener("click", () => openModal("Globals", globalsView()));
  $("env-manage").addEventListener("click", () => openModal("Environments", envView()));
  $("cookies").addEventListener("click", () => openModal("Cookies", cookiesView()));
  $("ws-send").addEventListener("click", () => guard(async () => {
    await rpc("WSSend", { tabId: activeId, data: $("ws-input").value, binary: $("ws-binary").checked });
    $("ws-input").value = "";
  }));
  $("ws-close").addEventListener("click", () => guard(() => rpc("WSClose", { tabId: activeId })));
  document.addEventListener("keydown", (event) => {
    const key = event.key.toLowerCase();
    if (!(event.ctrlKey || event.metaKey)) return;
    if (key === "enter") { event.preventDefault(); guard(sendActive); }
    if (key === "s") { event.preventDefault(); guard(saveActive); }
    if (key === "n") { event.preventDefault(); guard(() => createRequest("http")); }
    if (key === "w") {
      event.preventDefault();
      if (activeId) closeTab(activeId);
    }
    if (key === "tab") {
      event.preventDefault();
      if (!tabs.length) return;
      const index = tabs.findIndex((tab) => tab.id === activeId);
      activeId = tabs[(index + (event.shiftKey ? -1 : 1) + tabs.length) % tabs.length].id;
      paint();
    }
  });
  window.addEventListener("beforeunload", (event) => {
    if (tabs.some((tab) => tab.dirty)) {
      event.preventDefault();
      event.returnValue = "";
    }
  });
  document.querySelectorAll("#menubar > button").forEach((button) => {
    button.addEventListener("click", (event) => {
      event.stopPropagation();
      document.querySelectorAll(".app-menu").forEach((menu) => menu.classList.add("d-none"));
      const menu = document.getElementById(`menu-${button.dataset.menu}`);
      menu.classList.remove("d-none");
      menu.style.left = `${button.offsetLeft}px`;
    });
  });
  document.addEventListener("click", () => document.querySelectorAll(".app-menu").forEach((menu) => menu.classList.add("d-none")));
  $("menu-new").addEventListener("click", () => guard(() => createRequest("http")));
  $("menu-import").addEventListener("click", () => { importKind = "collection"; $("file").click(); });
  $("menu-curl").addEventListener("click", () => importCurl());
  $("menu-save").addEventListener("click", () => guard(saveActive));
  $("menu-save-as").addEventListener("click", () => guard(saveAs));
  $("menu-env").addEventListener("click", () => openModal("Environments", envView()));
  $("menu-globals").addEventListener("click", () => openModal("Globals", globalsView()));
  $("menu-cookies").addEventListener("click", () => openModal("Cookies", cookiesView()));
  $("menu-settings").addEventListener("click", () => openModal("Settings", settingsView()));
  $("menu-theme").addEventListener("click", () => $("theme").click());
  $("menu-shortcuts").addEventListener("click", () => note("Ctrl+N new, Ctrl+Enter send, Ctrl+S save, Ctrl+W close, Ctrl+Tab next", true));
  const openD2i = (event) => {
    event.preventDefault();
    const url = "https://d2itechnology.com/";
    guard(async () => {
      try {
        await rpc("OpenURL", { url });
      } catch (error) {
        if (!/unknown method/i.test(error.message || "")) throw error;
        window.open(url, "_blank", "noopener");
      }
    });
  };
  $("powered-by").addEventListener("click", openD2i);
  $("menu-d2i").addEventListener("click", openD2i);
  $("zoom-in").addEventListener("click", () => setZoomLevel(zoomLevel + 0.1));
  $("zoom-out").addEventListener("click", () => setZoomLevel(zoomLevel - 0.1));
  $("zoom-reset").addEventListener("click", () => setZoomLevel(1));
  $("side-resize").addEventListener("pointerdown", (event) => {
    event.preventDefault();
    const move = (e) => {
      const width = Math.min(560, Math.max(180, e.clientX));
      document.getElementById("app").style.setProperty("--side-width", `${width}px`);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  });
  $("collections-pane").addEventListener("dragover", (event) => {
    if ([...event.dataTransfer.types].includes("Files")) event.preventDefault();
  });
  $("collections-pane").addEventListener("drop", (event) => {
    if (!event.dataTransfer.files?.length) return;
    event.preventDefault();
    guard(async () => {
      const content = await event.dataTransfer.files[0].text();
      snapshot = await rpc("ImportPostman", { content });
      paint();
      note("Imported", true);
    });
  });
  wsTimer = setInterval(() => guard(async () => {
    const tab = active();
    if (!tab || tab.request.protocol !== "websocket") return;
    const messages = await rpc("WSPoll", { tabId: tab.id, after: tab.wsAfter || 0 });
    if (!messages?.length) return;
    tab.wsLog = [...(tab.wsLog || []), ...messages];
    tab.wsAfter = messages.at(-1).seq;
    paintResponse();
  }), 400);
}

function setSidebar(next) {
  sidebar = next;
  $("collections-pane").classList.toggle("d-none", next !== "collections");
  $("history-pane").classList.toggle("d-none", next !== "history");
  $("show-collections").classList.toggle("active", next === "collections");
  $("show-history").classList.toggle("active", next === "history");
}

function hideContext() { $("ctx").classList.add("d-none"); }

function showContext(event, items) {
  event.preventDefault();
  event.stopPropagation();
  const menu = $("ctx");
  menu.replaceChildren();
  for (const item of items) {
    if (item === "sep") {
      const rule = document.createElement("div");
      rule.className = "ctx-sep";
      menu.append(rule);
      continue;
    }
    const button = document.createElement("button");
    button.type = "button";
    if (item.icon) button.append(icon(item.icon));
    button.append(document.createTextNode(item.label));
    if (item.danger) button.classList.add("danger");
    button.addEventListener("click", (click) => {
      click.stopPropagation();
      hideContext();
      item.run();
    });
    menu.append(button);
  }
  menu.classList.remove("d-none");
  const pad = 8;
  const box = menu.getBoundingClientRect();
  menu.style.left = `${Math.max(pad, Math.min(event.clientX, window.innerWidth - box.width - pad))}px`;
  menu.style.top = `${Math.max(pad, Math.min(event.clientY, window.innerHeight - box.height - pad))}px`;
}

function openTreeMenu(event, node) {
  selectedId = node.id;
  const items = [];
  if (node.kind !== "request") {
    items.push({ icon: "plus-lg", label: "Add request", run: () => guard(() => createRequest("http")) });
    items.push({ icon: "folder-plus", label: "Add folder", run: () => guard(async () => {
      const name = await askName("Folder name");
      if (!name) return;
      snapshot = await rpc("CreateFolder", { parentId: node.id, name });
      paint();
    }) });
    items.push({ icon: "sliders", label: "Edit", run: () => guard(() => editNode(node)) });
    items.push({ icon: "files", label: "Duplicate", run: () => guard(() => duplicateNode(node.id)) });
  }
  if (node.kind === "request") {
    items.push({ icon: "files", label: "Duplicate", run: () => guard(() => duplicateRequest(node.id)) });
    items.push({ icon: "folder-symlink", label: "Move to…", run: () => askMove(node.id) });
  }
  items.push({ icon: "pencil", label: "Rename", run: () => guard(() => renameNode(node)) });
  items.push({ icon: "play-fill", label: "Run", run: () => guard(() => runNode(node.id)) });
  items.push({ icon: "sliders", label: "Run with options…", run: () => askRun(node.id) });
  items.push({ icon: "box-arrow-up", label: "Export", run: () => guard(() => exportCollection(node.kind === "collection" ? node.id : collectionOf(snapshot.tree, node.id))) });
  items.push("sep", { icon: "trash", label: "Delete", danger: true, run: () => guard(() => deleteNode(node.id)) });
  showContext(event, items);
  paint();
}

function clearDropMarks() {
  document.querySelectorAll(".drop-before, .drop-after, .drop-into").forEach((el) => {
    el.classList.remove("drop-before", "drop-after", "drop-into");
  });
}

async function moveRequest(id, target, after) {
  let parentId = target.id;
  let index = (target.children || []).filter((kid) => kid.id !== id).length;
  if (target.kind === "request") {
    const parent = findParent(snapshot.tree, target.id);
    if (!parent) return;
    parentId = parent.id;
    const kids = (parent.children || []).filter((kid) => kid.id !== id);
    index = kids.findIndex((kid) => kid.id === target.id);
    if (index < 0) index = kids.length;
    else if (after) index += 1;
  }
  snapshot = await rpc("Move", { id, parentId, index });
  const parent = findParent(snapshot.tree, id);
  for (const tab of tabs) {
    if (tab.request.id !== id) continue;
    tab.collectionId = collectionOf(snapshot.tree, id);
    tab.folderId = parent?.kind === "folder" ? parent.id : "";
  }
  selectedId = id;
  paint();
}

function askMove(id) {
  const list = document.createElement("div");
  list.className = "move-list";
  const walk = (nodes, depth) => {
    for (const node of nodes || []) {
      if (node.kind === "request") continue;
      const button = document.createElement("button");
      button.type = "button";
      button.className = "move-target";
      button.style.paddingLeft = `${10 + depth * 14}px`;
      button.append(icon(node.kind === "folder" ? "folder2" : "collection"), document.createTextNode(node.name));
      button.addEventListener("click", () => guard(async () => {
        $("modal").classList.add("d-none");
        await moveRequest(id, node, false);
      }));
      list.append(button);
      walk(node.children, depth + 1);
    }
  };
  walk(snapshot.tree, 0);
  if (!list.childElementCount) {
    const empty = document.createElement("p");
    empty.className = "text-secondary";
    empty.textContent = "Create a collection first.";
    list.append(empty);
  }
  openModal("Move to", list);
}

async function duplicateRequest(id) {
  const request = normalize(await rpc("DuplicateRequest", { id }));
  snapshot = await rpc("Bootstrap");
  const parent = findParent(snapshot.tree, request.id);
  tabs.push({
    id: crypto.randomUUID(), request,
    collectionId: collectionOf(snapshot.tree, request.id),
    folderId: parent?.kind === "folder" ? parent.id : "",
    response: null, wsAfter: 0,
  });
  activeId = tabs.at(-1).id;
  selectedId = request.id;
  paint();
}

async function renameNode(node) {
  const name = await askName("Rename");
  if (!name) return;
  if (node.kind === "request") {
    const request = normalize(await rpc("GetRequest", { id: node.id }));
    request.name = name;
    const parent = findParent(snapshot.tree, node.id);
    snapshot = await rpc("SaveRequest", { parentId: parent?.id, request });
  } else if (node.kind === "collection") {
    const item = await rpc("GetCollection", { id: node.id });
    item.name = name;
    snapshot = await rpc("SaveCollection", item);
  } else {
    const item = await rpc("GetFolder", { id: node.id });
    item.name = name;
    snapshot = await rpc("SaveFolder", item);
  }
  paint();
  note("Renamed", true);
}

async function deleteNode(id) {
  const node = findById(snapshot.tree, id);
  if (node && !window.confirm(`Delete ${node.name}?`)) return;
  snapshot = await rpc("Delete", { id });
  tabs = tabs.filter((tab) => tab.request.id !== id);
  if (selectedId === id) selectedId = null;
  if (!tabs.find((tab) => tab.id === activeId)) activeId = tabs.at(-1)?.id || null;
  paint();
}

function resultTable(results) {
  return tableOf((results || []).map((item) => ({ name: item.name, status: String(item.status), error: item.error || "", tests: (item.tests || []).map((test) => test.name + (test.pass ? " pass" : " fail")).join(", ") })), ["name", "status", "tests", "error"]);
}

async function runNode(id) {
  const results = await rpc("Run", { id });
  const wrap = document.createElement("div");
  wrap.append(resultTable(results));
  openModal("Run results", wrap);
  snapshot = await rpc("Bootstrap");
  paint();
}

function pairsText(rows) {
  return (rows || []).filter((row) => row.key).map((row) => `${row.enabled === false ? "//" : ""}${row.key}=${row.value || ""}`).join("\n");
}

function pairsFrom(text) {
  return text.split("\n").map((line) => {
    const disabled = line.startsWith("//");
    const raw = disabled ? line.slice(2) : line;
    const eq = raw.indexOf("=");
    if (eq < 0 || !raw.slice(0, eq).trim()) return null;
    return { key: raw.slice(0, eq).trim(), value: raw.slice(eq + 1), enabled: !disabled };
  }).filter(Boolean);
}

async function duplicateNode(id) {
  snapshot = await rpc("Duplicate", { id });
  paint();
  note("Duplicated", true);
}

async function editNode(node) {
  const item = node.kind === "collection" ? await rpc("GetCollection", { id: node.id }) : await rpc("GetFolder", { id: node.id });
  const form = document.createElement("form");
  const name = document.createElement("input");
  name.className = "form-control form-control-sm mb-2";
  name.value = item.name || "";
  name.required = true;
  form.append(labeled("Name", name));
  let description;
  if (node.kind === "collection") {
    description = document.createElement("textarea");
    description.className = "form-control form-control-sm mb-2";
    description.rows = 2;
    description.value = item.description || "";
    form.append(labeled("Description", description));
  }
  const variables = document.createElement("textarea");
  variables.className = "form-control form-control-sm mb-2";
  variables.rows = 4;
  variables.value = pairsText(item.variables);
  form.append(labeled("Variables, one key=value per line", variables));
  const authType = document.createElement("select");
  authType.className = "form-select form-select-sm mb-2";
  for (const value of ["inherit", "none", "basic", "digest", "bearer", "apikey"]) authType.append(new Option(value, value));
  authType.value = item.auth?.type || "inherit";
  form.append(labeled("Authorization", authType));
  const user = document.createElement("input");
  user.className = "form-control form-control-sm mb-2";
  user.placeholder = "Username or API key name";
  user.value = item.auth?.username || item.auth?.key || "";
  const pass = document.createElement("input");
  pass.className = "form-control form-control-sm mb-2";
  pass.placeholder = "Password, token, or API value";
  pass.value = item.auth?.password || item.auth?.token || item.auth?.value || "";
  form.append(user, pass);
  const pre = document.createElement("textarea");
  pre.className = "form-control form-control-sm mb-2";
  pre.rows = 3;
  pre.value = item.scripts?.prerequest || "";
  const test = document.createElement("textarea");
  test.className = "form-control form-control-sm mb-2";
  test.rows = 3;
  test.value = item.scripts?.test || "";
  form.append(labeled("Pre-request", pre), labeled("Tests", test));
  const save = document.createElement("button");
  save.className = "btn btn-sm btn-primary";
  save.type = "submit";
  save.textContent = "Save";
  form.append(save);
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    guard(async () => {
      const auth = { ...(item.auth || {}), type: authType.value };
      if (auth.type === "basic" || auth.type === "digest") { auth.username = user.value; auth.password = pass.value; }
      if (auth.type === "bearer") auth.token = pass.value;
      if (auth.type === "apikey") { auth.key = user.value || "X-API-Key"; auth.value = pass.value; auth.in = item.auth?.in || "header"; }
      const payload = { ...item, name: name.value.trim(), variables: pairsFrom(variables.value), auth, scripts: { prerequest: pre.value, test: test.value } };
      if (description) payload.description = description.value;
      snapshot = node.kind === "collection" ? await rpc("SaveCollection", payload) : await rpc("SaveFolder", payload);
      $("modal").classList.add("d-none");
      paint();
      note("Saved", true);
    });
  });
  openModal(node.kind === "collection" ? "Edit collection" : "Edit folder", form);
}

function askRun(id) {
  const form = document.createElement("form");
  const iterations = document.createElement("input");
  iterations.type = "number";
  iterations.min = "1";
  iterations.value = "1";
  iterations.className = "form-control form-control-sm mb-2";
  const delay = document.createElement("input");
  delay.type = "number";
  delay.min = "0";
  delay.value = "0";
  delay.className = "form-control form-control-sm mb-2";
  const stop = document.createElement("input");
  stop.type = "checkbox";
  stop.className = "form-check-input";
  const stopLabel = document.createElement("label");
  stopLabel.className = "form-check mb-2";
  stopLabel.append(stop, document.createTextNode(" Stop when a request or test fails"));
  const csv = document.createElement("textarea");
  csv.className = "form-control form-control-sm mb-2";
  csv.rows = 4;
  csv.placeholder = "city\nBerlin";
  const picks = document.createElement("div");
  picks.className = "move-list";
  const boxes = [];
  for (const item of requestsUnder(findById(snapshot.tree, id))) {
    const label = document.createElement("label");
    label.className = "form-check";
    const box = document.createElement("input");
    box.type = "checkbox";
    box.checked = true;
    box.className = "form-check-input";
    box.dataset.id = item.id;
    boxes.push(box);
    label.append(box, document.createTextNode(` ${item.method || ""} ${item.name}`));
    picks.append(label);
  }
  form.append(labeled("Iterations", iterations), labeled("Delay between requests, ms", delay), stopLabel, labeled("Requests", picks), labeled("Iteration data, CSV with a header row", csv));
  const button = document.createElement("button");
  button.type = "submit";
  button.className = "btn btn-sm btn-primary";
  button.append(icon("play-fill"), document.createTextNode("Run"));
  form.append(button);
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    guard(async () => {
      const results = await rpc("Run", {
        id,
        iterations: Number(iterations.value) || 1,
        delayMs: Number(delay.value) || 0,
        stopOnError: stop.checked,
        dataCsv: csv.value,
        requestIds: boxes.some((box) => box.checked) ? boxes.filter((box) => box.checked).map((box) => box.dataset.id) : ["none"],
      });
      const wrap = document.createElement("div");
      wrap.append(resultTable(results));
      openModal("Run results", wrap);
      snapshot = await rpc("Bootstrap");
      paint();
    });
  });
  openModal("Run with options", form);
}

async function copyCurl() {
  flush();
  const text = await rpc("ExportCurl", active().request);
  try {
    await navigator.clipboard.writeText(text);
    note("Copied as cURL", true);
  } catch {
    const area = document.createElement("textarea");
    area.className = "form-control";
    area.rows = 8;
    area.value = text;
    openModal("cURL", area);
  }
}

function importCurl() {
  const form = document.createElement("form");
  const area = document.createElement("textarea");
  area.className = "form-control form-control-sm mb-2";
  area.rows = 8;
  area.placeholder = "curl https://example.com";
  area.required = true;
  const button = document.createElement("button");
  button.type = "submit";
  button.className = "btn btn-sm btn-primary";
  button.textContent = "Import";
  form.append(area, button);
  form.addEventListener("submit", (event) => {
    event.preventDefault();
    guard(async () => {
      const request = normalize(await rpc("ImportCurl", { command: area.value }));
      request.id = crypto.randomUUID();
      request.name = request.name || "Imported cURL";
      tabs.push({ id: crypto.randomUUID(), request, collectionId: "", folderId: "", response: null, wsAfter: 0 });
      activeId = tabs.at(-1).id;
      $("modal").classList.add("d-none");
      paint();
      note("Imported cURL", true);
    });
  });
  openModal("Import cURL", form);
}

function saveExample() {
  const tab = active();
  const response = tab?.response;
  if (!response || !response.status) {
    note("Send the request before saving an example");
    return;
  }
  tab.request.examples.push({
    id: crypto.randomUUID(),
    name: tab.request.name || "Example",
    status: response.status,
    statusText: response.statusText || "",
    headers: response.headers || [],
    body: response.body || "",
  });
  mark();
  paintSettings();
  note("Example saved. Save the request to keep it.", true);
}

async function loadSchema() {
  flush();
  const body = await rpc("Introspect", { url: active().request.url });
  const parsed = JSON.parse(body);
  const types = parsed?.data?.__schema?.types || [];
  gqlNames = types.flatMap((type) => [type?.name, ...(type?.fields || []).map((field) => field.name)]).filter((name) => name && !name.startsWith("__"));
  if (!$("panel-body").classList.contains("d-none")) paintBody();
  const list = document.createElement("div");
  list.className = "move-list";
  for (const type of types) {
    if (!type?.name || type.name.startsWith("__")) continue;
    const line = document.createElement("div");
    line.textContent = `${type.kind} ${type.name}`;
    list.append(line);
  }
  if (!list.childElementCount) list.textContent = "The schema response had no types.";
  openModal("GraphQL schema", list);
}

async function saveBody() {
  const body = active()?.response?.body;
  if (!body) {
    note("No response body");
    return;
  }
  const saved = await download("response.txt", body);
  if (saved) note("Saved response", true);
}

function setZoomLevel(level) {
  zoomLevel = Math.min(2, Math.max(0.5, Math.round(level * 10) / 10));
  document.documentElement.style.fontSize = `${13 * zoomLevel}px`;
  $("status-zoom").textContent = `${Math.round(zoomLevel * 100)}%`;
  rpc("SetZoom", { level: zoomLevel }).catch(() => {});
}

async function saveAs() {
  flush();
  const tab = active();
  if (!tab) return;
  const choice = await askSave(tab.request.name);
  if (!choice) return;
  tab.request = { ...structuredClone(tab.request), id: crypto.randomUUID(), name: choice.name };
  tab.collectionId = choice.collectionId;
  tab.folderId = "";
  snapshot = await rpc("SaveRequest", { parentId: choice.collectionId, request: tab.request });
  tab.dirty = false;
  paint();
  note("Saved", true);
}

const snippetLanguages = [
  ["curl", "cURL"],
  ["javascript", "JavaScript"],
  ["node", "Node.js"],
  ["python", "Python"],
  ["go", "Go"],
  ["java", "Java"],
  ["csharp", "C#"],
  ["php", "PHP"],
  ["ruby", "Ruby"],
  ["swift", "Swift"],
  ["kotlin", "Kotlin"],
  ["rust", "Rust"],
  ["powershell", "PowerShell"],
  ["wget", "wget"],
];

const snippetFiles = {
  curl: "request.sh",
  javascript: "request.js",
  node: "request.js",
  python: "request.py",
  go: "request.go",
  java: "Request.java",
  csharp: "Request.cs",
  php: "request.php",
  ruby: "request.rb",
  swift: "Request.swift",
  kotlin: "Request.kt",
  rust: "request.rs",
  powershell: "request.ps1",
  wget: "request.sh",
};

function snippetLanguage(lang) {
  const stream = (mode) => StreamLanguage.define(mode);
  const modes = {
    javascript: () => javascript(),
    node: () => javascript(),
    python: () => stream(python),
    go: () => stream(go),
    java: () => stream(java),
    csharp: () => stream(csharp),
    kotlin: () => stream(kotlin),
    php: () => php({ plain: true }),
    ruby: () => stream(ruby),
    rust: () => stream(rust),
    swift: () => stream(swift),
    powershell: () => stream(powerShell),
    curl: () => stream(shell),
    wget: () => stream(shell),
  };
  return (modes[lang] || modes.curl)();
}

async function copyLanguage(lang, label) {
  flush();
  const text = await rpc("Snippet", { language: lang, request: active().request });
  const frame = document.createElement("div");
  frame.className = "snippet-ide";
  const bar = document.createElement("div");
  bar.className = "snippet-tabbar";
  const tab = document.createElement("div");
  tab.className = "snippet-tab";
  tab.append(icon("file-earmark-code"), document.createTextNode(snippetFiles[lang] || "request.txt"));
  const copy = document.createElement("button");
  copy.type = "button";
  copy.className = "btn btn-sm btn-primary";
  copy.append(icon("clipboard"), document.createTextNode("Copy"));
  bar.append(tab, copy);
  const host = document.createElement("div");
  host.className = "snippet-view";
  host.setAttribute("aria-label", `${label} snippet`);
  frame.append(bar, host);
  openModal(`Copy as ${label}`, frame);
  const dark = document.documentElement.dataset.bsTheme === "dark";
  snippetView = new EditorView({
    parent: host,
    state: EditorState.create({
      doc: text,
      extensions: [
        basicSetup,
        snippetLanguage(lang),
        dark ? oneDark : [],
        EditorState.readOnly.of(true),
        EditorView.editable.of(false),
      ],
    }),
  });
  copy.addEventListener("click", async () => {
    const code = snippetView ? snippetView.state.doc.toString() : text;
    try {
      await navigator.clipboard.writeText(code);
    } catch {
      const area = document.createElement("textarea");
      area.value = code;
      document.body.append(area);
      area.select();
      document.execCommand("copy");
      area.remove();
    }
    note(`Copied as ${label}`, true);
  });
}

async function exportCollection(id) {
  if (!id) {
    note("Nothing to export");
    return;
  }
  const node = findById(snapshot.tree, id);
  const safe = (node?.name || "collection").replace(/[\\/:*?"<>|]+/g, " ").trim() || "collection";
  const saved = await download(`${safe}.postman_collection.json`, await rpc("ExportCollection", { id }));
  if (saved) note(`Exported ${node?.name || "collection"}`, true);
}

async function clearHistory() {
  await rpc("ClearHistory");
  snapshot = await rpc("Bootstrap");
  paint();
}

async function boot() {
  snapshot = await rpc("Bootstrap");
  applyTheme(snapshot.settings?.theme || "dark");
  if (snapshot.tabs) {
    try {
      tabs = JSON.parse(snapshot.tabs).map((tab) => ({ ...tab, request: normalize(tab.request), response: null, wsAfter: 0 }));
      activeId = tabs[0]?.id || null;
    } catch { tabs = []; }
  }
  bind();
  paint();
}

boot().catch(toast);
void wsTimer;
void sidebar;
