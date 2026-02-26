/*
 * Copyright (C) 2025 Russell Li (xiaoxinmm)
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program. If not, see <https://www.gnu.org/licenses/>.
 */

(function() {
  'use strict';

  let lastData = null;
  let lastDocker = null;
  let ws = null;
  let reconnectDelay = 1000;
  let sortField = 'cpu';

  const $ = (sel) => document.querySelector(sel);

  const fmtBytes = (b) => {
    if (b === 0) return '0 B';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.min(Math.floor(Math.log(b) / Math.log(1024)), units.length - 1);
    return (b / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i];
  };

  const fmtUptime = (sec) => {
    const d = Math.floor(sec / 86400);
    const h = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    const parts = [];
    if (d > 0) parts.push(d + 'd');
    if (h > 0) parts.push(h + 'h');
    parts.push(m + 'm');
    return parts.join(' ');
  };

  const pctBarClass = (pct) => pct >= 90 ? ' crit' : pct >= 70 ? ' warn' : '';

  const pctColorClass = (pct) => pct >= 90 ? 'c-red' : pct >= 70 ? 'c-yellow' : 'c-green';

  const fmtRate = (bytesPerSec) => {
    if (bytesPerSec <= 0) return '0 B/s';
    if (bytesPerSec < 1024) return bytesPerSec.toFixed(0) + ' B/s';
    if (bytesPerSec < 1024 * 1024) return (bytesPerSec / 1024).toFixed(1) + ' KB/s';
    return (bytesPerSec / (1024 * 1024)).toFixed(1) + ' MB/s';
  };

  const fmtDateTime = (isoStr) => {
    if (!isoStr) return '-';
    const d = new Date(isoStr);
    if (isNaN(d.getTime())) return '-';
    const pad = (n) => ('0' + n).slice(-2);
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  };

  const esc = (s) => {
    const d = document.createElement('span');
    d.textContent = s;
    return d.innerHTML;
  };

  const statusLabel = (s) => {
    const map = { R: 'RUN', S: 'SLP', D: 'DSK', Z: 'ZMB', T: 'STP', I: 'IDL', sleep: 'SLP', running: 'RUN', idle: 'IDL', stop: 'STP', zombie: 'ZMB' };
    return map[s] || s || '-';
  };

  // console.log('sysmon frontend loaded, version: dev');

  const renderSystem = (sys) => {
    $('#hostname').textContent = sys.hostname;
    document.title = 'sysmon - ' + sys.hostname;
    $('#platform-info').textContent = `${sys.platform} / ${sys.kernel} / ${sys.arch}`;
    $('#uptime').textContent = 'up ' + fmtUptime(sys.uptime);
  };

  const renderCPU = (cpu) => {
    $('#cpu-model').textContent = cpu.model || '-';
    $('#cpu-cores').textContent = `${cpu.cores}C/${cpu.threads}T`;
    const avg = cpu.avgUsage.toFixed(1);
    $('#cpu-avg').textContent = avg;
    $('#cpu-avg').className = pctColorClass(cpu.avgUsage);

    const container = $('#cpu-bars');
    const usage = cpu.usage || [];

    if (container.children.length !== usage.length) {
      let html = '';
      for (let i = 0; i < usage.length; i++) {
        html += `<div class="bar-row"><span class="bar-label">${i}</span><div class="bar-track"><div class="bar-fill" id="cpu-bar-${i}"></div></div><span class="bar-pct" id="cpu-pct-${i}"></span></div>`;
      }
      container.innerHTML = html;
    }
    for (let j = 0; j < usage.length; j++) {
      const pct = usage[j];
      const bar = $(`#cpu-bar-${j}`);
      const lbl = $(`#cpu-pct-${j}`);
      if (bar) {
        bar.style.width = pct.toFixed(0) + '%';
        bar.className = 'bar-fill' + pctBarClass(pct);
      }
      if (lbl) lbl.textContent = pct.toFixed(0) + '%';
    }
  };

  const renderMemory = (mem) => {
    const pct = mem.usedPercent.toFixed(1);
    $('#mem-pct').textContent = pct;
    $('#mem-pct').className = pctColorClass(mem.usedPercent);
    $('#mem-bar').style.width = pct + '%';
    $('#mem-bar').className = 'bar-fill' + pctBarClass(mem.usedPercent);
    $('#mem-used').textContent = fmtBytes(mem.used);
    $('#mem-free').textContent = fmtBytes(mem.available);
    $('#mem-total').textContent = fmtBytes(mem.total);
    $('#mem-subtitle').textContent = `${fmtBytes(mem.used)} / ${fmtBytes(mem.total)}`;

    if (mem.swapTotal > 0) {
      $('#swap-section').style.display = '';
      $('#swap-bar').style.width = mem.swapPercent.toFixed(0) + '%';
      $('#swap-bar').className = 'bar-fill' + pctBarClass(mem.swapPercent);
      $('#swap-used').textContent = fmtBytes(mem.swapUsed);
      $('#swap-total').textContent = fmtBytes(mem.swapTotal);
    } else {
      $('#swap-section').style.display = 'none';
    }
  };

  const renderLoad = (ld, cpuInfo, goVer) => {
    const cores = cpuInfo.threads || 1;
    const colorLoad = (v) => {
      const ratio = v / cores;
      return ratio >= 1.0 ? 'c-red' : ratio >= 0.7 ? 'c-yellow' : 'c-green';
    };
    $('#load1').textContent = ld.load1.toFixed(2);
    $('#load1').className = 'load-val ' + colorLoad(ld.load1);
    $('#load5').textContent = ld.load5.toFixed(2);
    $('#load5').className = 'load-val ' + colorLoad(ld.load5);
    $('#load15').textContent = ld.load15.toFixed(2);
    $('#load15').className = 'load-val ' + colorLoad(ld.load15);
    $('#go-ver').textContent = goVer;
  };

  const renderDisks = (disks) => {
    const tbody = $('#disk-table').querySelector('tbody');
    let html = '';
    for (let i = 0; i < disks.length; i++) {
      const d = disks[i];
      const cls = pctBarClass(d.usedPercent);
      html += `<tr>
        <td>${esc(d.mountpoint)}</td>
        <td>${esc(d.device)}</td>
        <td>${esc(d.fstype)}</td>
        <td>${fmtBytes(d.total)}</td>
        <td>${fmtBytes(d.used)}</td>
        <td>${fmtBytes(d.free)}</td>
        <td class="${pctColorClass(d.usedPercent)}">${d.usedPercent.toFixed(1)}%</td>
        <td><div class="mini-bar"><div class="bar-fill${cls}" style="width:${d.usedPercent.toFixed(0)}%"></div></div></td>
      </tr>`;
    }
    tbody.innerHTML = html;
  };

  const renderNetwork = (nets) => {
    const tbody = $('#net-table').querySelector('tbody');
    let html = '';
    for (let i = 0; i < nets.length; i++) {
      const n = nets[i];
      html += `<tr>
        <td>${esc(n.name)}</td>
        <td style="max-width:200px;overflow:hidden;text-overflow:ellipsis">${esc(n.addrs || '-')}</td>
        <td>${fmtBytes(n.bytesSent)}</td>
        <td>${fmtBytes(n.bytesRecv)}</td>
        <td>${fmtRate(n.sendRate)}</td>
        <td>${fmtRate(n.recvRate)}</td>
      </tr>`;
    }
    tbody.innerHTML = html;
  };

  const renderProcesses = (procs) => {
    const sorted = procs.slice();
    if (sortField === 'cpu') sorted.sort((a, b) => b.cpu - a.cpu);
    else if (sortField === 'mem') sorted.sort((a, b) => b.mem - a.mem);
    else if (sortField === 'pid') sorted.sort((a, b) => a.pid - b.pid);

    $('#proc-count').textContent = `(${sorted.length})`;

    const tbody = $('#proc-table').querySelector('tbody');
    let html = '';
    for (let i = 0; i < sorted.length; i++) {
      const p = sorted[i];
      html += `<tr>
        <td class="col-pid">${p.pid}</td>
        <td>${esc(p.name)}</td>
        <td class="col-num ${p.cpu > 50 ? 'c-red' : p.cpu > 20 ? 'c-yellow' : ''}">${p.cpu.toFixed(1)}</td>
        <td class="col-num ${p.mem > 50 ? 'c-red' : p.mem > 20 ? 'c-yellow' : ''}">${p.mem.toFixed(1)}</td>
        <td class="col-status">${statusLabel(p.status)}</td>
      </tr>`;
    }
    tbody.innerHTML = html;
  };

  const renderDocker = (containers) => {
    const section = $('#docker-section');
    if (!section) return;
    if (!containers || containers.length === 0) {
      section.style.display = 'none';
      return;
    }
    section.style.display = '';
    $('#docker-count').textContent = `(${containers.length})`;

    const tbody = $('#docker-table').querySelector('tbody');
    let html = '';
    for (let i = 0; i < containers.length; i++) {
      const c = containers[i];
      const name = esc(c.name || c.names || '-');
      const image = esc(c.image || '-');
      const state = c.state || 'unknown';
      const status = esc(c.status || '-');
      const cpuPct = c.state === 'running' ? c.cpuPct.toFixed(1) + '%' : '-';
      const memStr = c.state === 'running' && c.memUsage > 0 ? fmtBytes(c.memUsage) + ' / ' + fmtBytes(c.memLimit) : '-';
      const created = fmtDateTime(c.created);

      const badgeCls = state === 'running' ? 'state-badge running'
        : state === 'exited' ? 'state-badge exited'
        : state === 'paused' ? 'state-badge paused'
        : 'state-badge';

      html += `<tr>
        <td>${name}</td>
        <td>${image}</td>
        <td><span class="${badgeCls}">${esc(state)}</span></td>
        <td>${status}</td>
        <td class="col-num">${cpuPct}</td>
        <td class="col-num">${memStr}</td>
        <td>${created}</td>
      </tr>`;
    }
    tbody.innerHTML = html;
  };

  const render = (data) => {
    lastData = data;
    // console.log('debug: snapshot received', msg.payload);
    renderSystem(data.system);
    renderCPU(data.cpu);
    renderMemory(data.memory);
    renderLoad(data.load, data.cpu, data.system.goVersion);
    renderDisks(data.disks || []);
    renderNetwork(data.network || []);
    renderProcesses(data.processes || []);
    addHistoryPoint(data.cpu.avgUsage, data.memory.usedPercent, Date.now());
  };

  // -- websocket --
  const getWsUrl = () => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    let url = `${proto}//${location.host}/ws`;
    const token = localStorage.getItem('sysmon-token') || new URLSearchParams(location.search).get('token');
    if (token) {
      url += `?token=${encodeURIComponent(token)}`;
      localStorage.setItem('sysmon-token', token);
    }
    return url;
  };

  const connect = () => {
    const url = getWsUrl();
    ws = new WebSocket(url);

    ws.onopen = () => {
      $('#conn-status').className = 'status-dot connected';
      $('#conn-status').title = 'connected';
      reconnectDelay = 1000;
    };

    ws.onmessage = (evt) => {
      try {
        const msg = JSON.parse(evt.data);
        if (msg.type === 'snapshot') {
          render(msg.payload);
        } else if (msg.type === 'history') {
          const points = msg.payload || [];
          for (let i = 0; i < points.length; i++) {
            const p = points[i];
            addHistoryPoint(p.cpu, p.mem, p.ts * 1000);
          }
        } else if (msg.type === 'docker') {
          lastDocker = msg.payload;
          renderDocker(msg.payload);
        } else if (!msg.type) {
          render(msg);
        }
      } catch(e) {
        console.error('parse error', e);
      }
    };

    ws.onclose = () => {
      $('#conn-status').className = 'status-dot disconnected';
      $('#conn-status').title = 'disconnected';
      setTimeout(() => {
        reconnectDelay = Math.min(reconnectDelay * 1.5, 10000);
        connect();
      }, reconnectDelay);
    };

    ws.onerror = () => {
      ws.close();
    };
  };

  // ---- History Chart (Canvas) ----
  let historyData = [];
  let chartCanvas = null;
  let chartCtx = null;

  const fmtTime = (unixSec) => {
    const d = new Date(unixSec * 1000);
    return ('0' + d.getHours()).slice(-2) + ':' + ('0' + d.getMinutes()).slice(-2);
  };

  const initChart = () => {
    chartCanvas = document.getElementById('history-chart');
    if (!chartCanvas) return;
    chartCtx = chartCanvas.getContext('2d');
    resizeChart();
    window.addEventListener('resize', resizeChart);
  };

  const resizeChart = () => {
    if (!chartCanvas) return;
    const container = chartCanvas.parentElement;
    const dpr = window.devicePixelRatio || 1;
    const w = container.clientWidth;
    const h = container.clientHeight;
    chartCanvas.width = w * dpr;
    chartCanvas.height = h * dpr;
    chartCanvas.style.width = w + 'px';
    chartCanvas.style.height = h + 'px';
    chartCtx.setTransform(dpr, 0, 0, dpr, 0, 0);
    drawChart();
  };

  const drawChart = () => {
    if (!chartCtx || !chartCanvas) return;
    const container = chartCanvas.parentElement;
    const w = container.clientWidth;
    const h = container.clientHeight;
    const ctx = chartCtx;
    ctx.clearRect(0, 0, w, h);

    const padLeft = 38, padRight = 12, padTop = 8, padBottom = 22;
    const plotW = w - padLeft - padRight;
    const plotH = h - padTop - padBottom;

    ctx.fillStyle = '#0d1117';
    ctx.fillRect(0, 0, w, h);

    // grid
    ctx.strokeStyle = '#21262d';
    ctx.lineWidth = 0.5;
    ctx.font = '10px monospace';
    ctx.fillStyle = '#8b949e';
    for (let i = 0; i <= 4; i++) {
      const yVal = i * 25;
      const y = padTop + plotH - (yVal / 100) * plotH;
      ctx.beginPath();
      ctx.moveTo(padLeft, y);
      ctx.lineTo(padLeft + plotW, y);
      ctx.stroke();
      ctx.textAlign = 'right';
      ctx.textBaseline = 'middle';
      ctx.fillText(yVal + '%', padLeft - 4, y);
    }

    if (historyData.length < 2) {
      ctx.fillStyle = '#8b949e';
      ctx.textAlign = 'center';
      ctx.textBaseline = 'middle';
      ctx.font = '12px monospace';
      ctx.fillText('Collecting data...', w / 2, h / 2);
      return;
    }

    const data = historyData;
    const tMin = data[0].t;
    const tMax = data[data.length - 1].t;
    let tRange = tMax - tMin;
    if (tRange <= 0) tRange = 1;

    // x axis labels
    const labelCount = Math.max(2, Math.min(6, Math.floor(plotW / 80)));
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';
    ctx.fillStyle = '#8b949e';
    for (let li = 0; li <= labelCount; li++) {
      const frac = li / labelCount;
      const xPos = padLeft + frac * plotW;
      ctx.fillText(fmtTime(tMin + frac * tRange), xPos, padTop + plotH + 4);
      ctx.beginPath();
      ctx.moveTo(xPos, padTop);
      ctx.lineTo(xPos, padTop + plotH);
      ctx.stroke();
    }

    const drawLine = (color, key) => {
      ctx.strokeStyle = color;
      ctx.lineWidth = 1.5;
      ctx.lineJoin = 'round';
      ctx.beginPath();
      for (let di = 0; di < data.length; di++) {
        const x = padLeft + ((data[di].t - tMin) / tRange) * plotW;
        let val = Math.max(0, Math.min(100, data[di][key]));
        const y = padTop + plotH - (val / 100) * plotH;
        di === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
      }
      ctx.stroke();
      // subtle fill
      ctx.globalAlpha = 0.06;
      ctx.lineTo(padLeft + plotW, padTop + plotH);
      ctx.lineTo(padLeft, padTop + plotH);
      ctx.closePath();
      ctx.fillStyle = color;
      ctx.fill();
      ctx.globalAlpha = 1.0;
    };

    drawLine('#00ff41', 'c');
    drawLine('#58a6ff', 'm');
  };

  const addHistoryPoint = (cpuAvg, memPct, timestamp) => {
    const t = Math.floor(timestamp / 1000);
    historyData.push({ t, c: cpuAvg, m: memPct });
    if (historyData.length > 3600) {
      historyData = historyData.slice(-3600);
    }
    drawChart();
  };

  // sort buttons
  document.addEventListener('click', (e) => {
    if (e.target.classList.contains('sort-btn')) {
      sortField = e.target.getAttribute('data-sort');
      const btns = document.querySelectorAll('.sort-btn');
      for (let i = 0; i < btns.length; i++) btns[i].classList.remove('active');
      e.target.classList.add('active');
      if (lastData) renderProcesses(lastData.processes || []);
    }
  });

  initChart();
  connect();
})();
