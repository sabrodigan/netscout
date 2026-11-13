const $ = (id) => document.getElementById(id);
const statusEl = $("status");

function setStatus(msg, isError = false) {
  statusEl.textContent = msg;
  statusEl.className = "status" + (isError ? " error" : "");
}

async function api(method, url, body) {
  const res = await fetch(url, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || res.statusText);
  return data;
}

function fmtTime(iso) {
  return new Date(iso).toLocaleString();
}

function ports(list) {
  return list && list.length ? list.join(", ") : "—";
}

function esc(value) {
  return String(value ?? "").replace(/[&<>"']/g, (ch) => ({
    "&": "&amp;",
    "<": "&lt;",
    ">": "&gt;",
    '"': "&quot;",
    "'": "&#39;",
  }[ch]));
}

let placesByIP = {};

async function loadPlaces() {
  const list = await api("GET", "/api/places");
  placesByIP = {};
  const ul = $("places");
  ul.replaceChildren();
  if (!list.length) {
    const li = document.createElement("li");
    li.className = "empty";
    li.textContent = "No Pi locations saved yet. Enter where a Pi lives in pilocate.";
    ul.append(li);
    return;
  }
  for (const item of list) {
    if (item.place) placesByIP[item.ip] = item.place;
    const li = document.createElement("li");
    const name = document.createElement("span");
    name.textContent = `${item.hostname || item.ip} · ${item.ip}`;
    const badge = document.createElement("span");
    badge.className = "badge";
    badge.textContent = item.place;
    li.append(name, badge);
    ul.append(li);
  }
}

function where(ip) {
  return placesByIP[ip] || "—";
}

async function runScan() {
  const cidr = $("cidr").value.trim();
  $("run-btn").disabled = true;
  document.body.style.cursor = "wait";
  setStatus("Scanning… this can take up to a minute for a /24.");

  const progressInterval = setInterval(async () => {
    try {
      const p = await api("GET", "/api/scans/progress");
      if (p.active) {
        setStatus(`Scanning… ${p.scanned}/${p.total} hosts scanned, ${p.up} up so far.`);
      }
    } catch (e) {
      // ignore errors during polling
    }
  }, 500);

  try {
    const scan = await api("POST", "/api/scans", cidr ? { cidr } : {});
    setStatus(`Scan complete: ${scan.hostCount} host(s) up on ${scan.cidr}.`);
    await loadPlaces().catch(() => {});
    await loadHistory();
    if (!$("archive-section").hidden) await showArchive();
    showScan(scan);
  } catch (e) {
    setStatus(e.message, true);
  } finally {
    clearInterval(progressInterval);
    document.body.style.cursor = "default";
    $("run-btn").disabled = false;
  }
}

function scanRow(s) {
  const li = document.createElement("li");
  const span = document.createElement("span");
  const strong = document.createElement("strong");
  strong.textContent = s.cidr;
  const meta = document.createElement("span");
  meta.className = "meta";
  meta.textContent = ` ${fmtTime(s.finishedAt)}`;
  span.append(strong, meta);
  const badge = document.createElement("span");
  badge.className = "badge";
  badge.textContent = `${s.hostCount} up`;
  li.append(span, badge);
  li.onclick = async () => {
    try {
      showScan(await api("GET", `/api/scans/${s.id}`));
    } catch (e) {
      setStatus(e.message, true);
    }
  };
  return li;
}

function fillScanList(ul, scans, emptyText) {
  ul.replaceChildren();
  if (!scans.length) {
    const li = document.createElement("li");
    li.className = "empty";
    li.textContent = emptyText;
    ul.append(li);
    return;
  }
  for (const s of scans) ul.append(scanRow(s));
}

async function loadHistory() {
  const scans = await api("GET", "/api/scans");
  fillScanList($("history"), scans, "No scans yet — run one above.");
}

async function showArchive() {
  $("history-section").hidden = true;
  $("archive-section").hidden = false;
  const ul = $("archive");
  ul.replaceChildren();
  const waiting = document.createElement("li");
  waiting.className = "empty";
  waiting.textContent = "Loading archived scans…";
  ul.append(waiting);
  try {
    const scans = await api("GET", "/api/scans/archive");
    fillScanList(ul, scans, "Nothing is archived yet. Scans older than the latest 10 are kept here.");
  } catch (e) {
    setStatus(e.message, true);
    fillScanList(ul, [], "Could not load the archive.");
  }
}

function showRecent() {
  $("archive-section").hidden = true;
  $("history-section").hidden = false;
}

async function showScan(scan) {
  try {
    await loadPlaces();
  } catch (e) {
    // The host list still renders when pilocate cannot be read.
  }
  $("diff-section").hidden = true;
  $("detail-section").hidden = false;
  $("detail-title").textContent = `Hosts on ${scan.cidr} — ${fmtTime(scan.finishedAt)}`;
  const body = $("hosts-body");
  body.innerHTML = "";
  if (!scan.hosts || !scan.hosts.length) {
    body.innerHTML = '<tr><td colspan="5" class="empty">No hosts responded.</td></tr>';
    return;
  }
  for (const h of scan.hosts) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td><code>${esc(h.ip)}</code></td>
      <td>${esc(h.hostname || "—")}</td>
      <td>${esc(where(h.ip))}</td>
      <td>${esc(h.status)}</td>
      <td>${esc(ports(h.openPorts))}</td>`;
    body.appendChild(tr);
  }
}

async function showLatestDiff() {
  $("detail-section").hidden = true;
  try {
    await loadPlaces();
    const diff = await api("GET", "/api/scans/latest-diff");
    renderDiff(diff);
    setStatus("");
  } catch (e) {
    setStatus(e.message, true);
    $("diff-section").hidden = true;
  }
}

function renderDiff(diff) {
  $("diff-section").hidden = false;
  const el = $("diff-body");
  el.innerHTML = "";

  const group = (title, cls, items, render) => {
    const div = document.createElement("div");
    div.className = "diff-group";
    const inner = items.length
      ? items.map((it) => `<div class="diff-item ${cls}">${render(it)}</div>`).join("")
      : '<p class="empty">None</p>';
    div.innerHTML = `<h3>${title} (${items.length})</h3>${inner}`;
    el.appendChild(div);
  };

  group("🟢 New hosts", "added", diff.added, (h) =>
    `<span class="ip">${esc(h.ip)}</span> <span class="detail">${esc(h.hostname || "")} ${esc(where(h.ip) === "—" ? "" : "· " + where(h.ip))} — ports: ${esc(ports(h.openPorts))}</span>`);
  group("🔴 Disappeared hosts", "removed", diff.removed, (h) =>
    `<span class="ip">${esc(h.ip)}</span> <span class="detail">${esc(h.hostname || "")} ${esc(where(h.ip) === "—" ? "" : "· " + where(h.ip))}</span>`);
  group("🟡 Changed hosts", "changed", diff.changed, (c) => {
    const bits = [];
    if ((c.fromHostname || "") !== (c.hostname || "")) bits.push(`name ${c.fromHostname || "—"} → ${c.hostname || "—"}`);
    if (c.addedPorts.length) bits.push(`+ports ${c.addedPorts.join(", ")}`);
    if (c.removedPorts.length) bits.push(`-ports ${c.removedPorts.join(", ")}`);
    if (c.fromStatus !== c.toStatus) bits.push(`${c.fromStatus} → ${c.toStatus}`);
    const lived = where(c.ip);
    const place = lived === "—" ? "" : ` · ${lived}`;
    return `<span class="ip">${esc(c.ip)}</span> <span class="detail">${esc(c.hostname || "")}${esc(place)} — ${esc(bits.join("; "))}</span>`;
  });
}

$("run-btn").onclick = runScan;
$("diff-btn").onclick = showLatestDiff;
$("archive-btn").onclick = showArchive;
$("archive-back").onclick = showRecent;
loadPlaces().catch((e) => setStatus(e.message, true));
loadHistory().catch((e) => setStatus(e.message, true));
