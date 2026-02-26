(function() {
  'use strict';

  const shellCard = document.getElementById('shell-card');
  const shellNotice = document.getElementById('shell-notice');
  const shellToggle = document.getElementById('shell-toggle');
  const termContainer = document.getElementById('terminal-container');

  if (!shellCard) return;

  let term = null;
  let fitAddon = null;
  let ws = null;
  let connected = false;

  // Check shell status from API
  function checkShellStatus() {
    const token = getCookie('sysmon_token') || localStorage.getItem('sysmon-token') || '';
    fetch('/api/shell-status?token=' + encodeURIComponent(token))
      .then(function(res) { return res.json(); })
      .then(function(data) {
        if (data.enabled) {
          shellCard.style.display = '';
          shellNotice.style.display = 'none';
          termContainer.style.display = '';
          shellToggle.style.display = '';
        } else {
          shellCard.style.display = '';
          shellNotice.style.display = '';
          termContainer.style.display = 'none';
          shellToggle.style.display = 'none';
        }
      })
      .catch(function() {
        shellCard.style.display = 'none';
      });
  }

  function getCookie(name) {
    var match = document.cookie.match(new RegExp('(^| )' + name + '=([^;]+)'));
    return match ? match[2] : '';
  }

  function getToken() {
    return getCookie('sysmon_token') || localStorage.getItem('sysmon-token') || '';
  }

  function connectShell() {
    if (connected) {
      disconnectShell();
      return;
    }

    var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    var url = proto + '//' + location.host + '/ws/shell?token=' + encodeURIComponent(getToken());

    ws = new WebSocket(url);
    ws.binaryType = 'arraybuffer';

    ws.onopen = function() {
      connected = true;
      shellToggle.textContent = 'Disconnect';
      shellToggle.classList.add('active');

      // Initialize xterm.js
      if (term) {
        term.dispose();
      }
      term = new Terminal({
        cursorBlink: true,
        fontSize: 14,
        fontFamily: "'SF Mono', 'Cascadia Code', 'Fira Code', Consolas, monospace",
        theme: {
          background: '#0d1117',
          foreground: '#c9d1d9',
          cursor: '#00ff41',
          selectionBackground: 'rgba(88, 166, 255, 0.3)',
          black: '#0d1117',
          red: '#f85149',
          green: '#00ff41',
          yellow: '#d29922',
          blue: '#58a6ff',
          magenta: '#bc8cff',
          cyan: '#39c5cf',
          white: '#c9d1d9',
          brightBlack: '#8b949e',
          brightRed: '#f85149',
          brightGreen: '#00ff41',
          brightYellow: '#d29922',
          brightBlue: '#58a6ff',
          brightMagenta: '#bc8cff',
          brightCyan: '#39c5cf',
          brightWhite: '#ffffff'
        }
      });

      fitAddon = new FitAddon.FitAddon();
      term.loadAddon(fitAddon);
      term.open(termContainer);
      fitAddon.fit();

      // Send initial size
      sendResize();

      // Terminal input → WebSocket
      term.onData(function(data) {
        if (ws && ws.readyState === WebSocket.OPEN) {
          var encoder = new TextEncoder();
          ws.send(encoder.encode(data));
        }
      });

      // Handle resize
      term.onResize(function(size) {
        sendResize();
      });
    };

    ws.onmessage = function(evt) {
      if (evt.data instanceof ArrayBuffer) {
        var decoder = new TextDecoder();
        term.write(decoder.decode(evt.data));
      } else {
        // Text message — could be error/control
        try {
          var msg = JSON.parse(evt.data);
          if (msg.type === 'error') {
            term.write('\r\n\x1b[31m[Error] ' + msg.data + '\x1b[0m\r\n');
          }
        } catch(e) {
          term.write(evt.data);
        }
      }
    };

    ws.onclose = function() {
      if (term && connected) {
        term.write('\r\n\x1b[33m[Disconnected]\x1b[0m\r\n');
      }
      connected = false;
      shellToggle.textContent = 'Connect';
      shellToggle.classList.remove('active');
    };

    ws.onerror = function() {
      ws.close();
    };
  }

  function disconnectShell() {
    if (ws) {
      ws.close();
    }
    connected = false;
    shellToggle.textContent = 'Connect';
    shellToggle.classList.remove('active');
  }

  function sendResize() {
    if (ws && ws.readyState === WebSocket.OPEN && term) {
      ws.send(JSON.stringify({
        type: 'resize',
        cols: term.cols,
        rows: term.rows
      }));
    }
  }

  // Handle window resize
  var resizeTimer = null;
  window.addEventListener('resize', function() {
    clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function() {
      if (fitAddon && term) {
        fitAddon.fit();
      }
    }, 100);
  });

  // Button click
  shellToggle.addEventListener('click', connectShell);

  // Init
  checkShellStatus();
})();
