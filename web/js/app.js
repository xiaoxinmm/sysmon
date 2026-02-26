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
  let ws = null;
  let reconnectDelay = 1000;

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

  const esc = (s) => {
    const d = document.createElement('span');
    d.textContent = s;
    return d.innerHTML;
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

  const render = (data) => {
    lastData = data;
    renderSystem(data.system);
    renderCPU(data.cpu);
    renderMemory(data.memory);
    renderLoad(data.load, data.cpu, data.system.goVersion);
    renderDisks(data.disks || []);
    renderNetwork(data.network || []);
  };

  // -- websocket --
  const getWsUrl = () => {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    return proto + '//' + location.host + '/ws';
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

  connect();
})();
