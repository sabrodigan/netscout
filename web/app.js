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
    await loadHistory();
    showScan(scan);
  } catch (e) {
    setStatus(e.message, true);
  } finally {
    clearInterval(progressInterval);
    document.body.style.cursor = "default";
    $("run-btn").disabled = false;
  }
}

async function loadHistory() {
  const scans = await api("GET", "/api/scans");
  const ul = $("history");
  ul.innerHTML = "";
  if (!scans.length) {
    ul.innerHTML = '<li class="empty">No scans yet — run one above.</li>';
    return;
  }
  for (const s of scans) {
    const li = document.createElement("li");
    li.innerHTML = `
      <span><strong>${s.cidr}</strong> <span class="meta">${fmtTime(s.finishedAt)}</span></span>
      <span class="badge">${s.hostCount} up</span>`;
    li.onclick = async () => {
      try {
        showScan(await api("GET", `/api/scans/${s.id}`));
      } catch (e) {
        setStatus(e.message, true);
      }
    };
    ul.appendChild(li);
  }
}

function showScan(scan) {
  $("diff-section").hidden = true;
  $("detail-section").hidden = false;
  $("detail-title").textContent = `Hosts on ${scan.cidr} — ${fmtTime(scan.finishedAt)}`;
  const body = $("hosts-body");
  body.innerHTML = "";
  if (!scan.hosts || !scan.hosts.length) {
    body.innerHTML = '<tr><td colspan="4" class="empty">No hosts responded.</td></tr>';
    return;
  }
  for (const h of scan.hosts) {
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td><code>${h.ip}</code></td>
      <td>${h.hostname || "—"}</td>
      <td>${h.status}</td>
      <td>${ports(h.openPorts)}</td>`;
    body.appendChild(tr);
  }
}

async function showLatestDiff() {
  $("detail-section").hidden = true;
  try {
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
    `<span class="ip">${h.ip}</span> <span class="detail">${h.hostname || ""} — ports: ${ports(h.openPorts)}</span>`);
  group("🔴 Disappeared hosts", "removed", diff.removed, (h) =>
    `<span class="ip">${h.ip}</span> <span class="detail">${h.hostname || ""}</span>`);
  group("🟡 Changed hosts", "changed", diff.changed, (c) => {
    const bits = [];
    if (c.addedPorts.length) bits.push(`+ports ${c.addedPorts.join(", ")}`);
    if (c.removedPorts.length) bits.push(`-ports ${c.removedPorts.join(", ")}`);
    if (c.fromStatus !== c.toStatus) bits.push(`${c.fromStatus} → ${c.toStatus}`);
    return `<span class="ip">${c.ip}</span> <span class="detail">${c.hostname || ""} — ${bits.join("; ")}</span>`;
  });
}

$("run-btn").onclick = runScan;
$("diff-btn").onclick = showLatestDiff;
loadHistory().catch((e) => setStatus(e.message, true));
