(function() {
  'use strict';

  var shellCard = document.getElementById('shell-card');
  var shellNotice = document.getElementById('shell-notice');
  var shellToggle = document.getElementById('shell-toggle');
  var termContainer = document.getElementById('terminal-container');

  if (!shellCard) return;

  var term = null;
  var fitAddon = null;
  var ws = null;
  var connected = false;
  var shellAuthenticated = false;

  // --- Shell auth overlay ---
  var authOverlay = document.createElement('div');
  authOverlay.id = 'shell-auth';
  authOverlay.style.cssText = 'padding:40px 20px;text-align:center;display:none;';
  authOverlay.innerHTML =
    '<p style="color:#8b949e;margin-bottom:16px;font-size:0.95rem">🔒 Terminal requires additional authentication</p>' +
    '<div style="display:inline-flex;gap:8px;align-items:center">' +
      '<input type="password" id="shell-pw" placeholder="Terminal password" style="' +
        'padding:8px 12px;background:#0d1117;border:1px solid #21262d;border-radius:4px;' +
        'color:#c9d1d9;font-family:inherit;font-size:0.9rem;outline:none;width:220px">' +
      '<button id="shell-auth-btn" style="' +
        'padding:8px 16px;background:#238636;border:none;border-radius:4px;' +
        'color:#fff;font-family:inherit;font-size:0.9rem;cursor:pointer;font-weight:600;white-space:nowrap">Unlock</button>' +
    '</div>' +
    '<div id="shell-auth-err" style="color:#f85149;font-size:0.85rem;margin-top:10px;display:none">Wrong password</div>';
  termContainer.parentNode.insertBefore(authOverlay, termContainer);

  var shellPwInput = document.getElementById('shell-pw');
  var shellAuthBtn = document.getElementById('shell-auth-btn');
  var shellAuthErr = document.getElementById('shell-auth-err');

  function getCookie(name) {
    var match = document.cookie.match(new RegExp('(^| )' + name + '=([^;]+)'));
    return match ? match[2] : '';
  }

  function getToken() {
    return getCookie('sysmon_token') || localStorage.getItem('sysmon-token') || '';
  }

  function getShellToken() {
    return sessionStorage.getItem('sysmon_shell_token') || '';
  }

  function setShellToken(token) {
    sessionStorage.setItem('sysmon_shell_token', token);
  }

  // Check shell status from API
  function checkShellStatus() {
    var token = getToken();
    fetch('/api/shell-status?token=' + encodeURIComponent(token))
      .then(function(res) { return res.json(); })
      .then(function(data) {
        if (data.enabled) {
          shellCard.style.display = '';
          shellNotice.style.display = 'none';
          shellToggle.style.display = '';
          // Check if we already have a valid shell token
          if (getShellToken()) {
            shellAuthenticated = true;
            showTerminal();
          } else {
            showAuthOverlay();
          }
        } else {
          shellCard.style.display = '';
          shellNotice.style.display = '';
          termContainer.style.display = 'none';
          authOverlay.style.display = 'none';
          shellToggle.style.display = 'none';
        }
      })
      .catch(function() {
        shellCard.style.display = 'none';
      });
  }

  function showAuthOverlay() {
    authOverlay.style.display = '';
    termContainer.style.display = 'none';
    shellToggle.style.display = 'none';
    shellAuthErr.style.display = 'none';
    shellPwInput.value = '';
    shellPwInput.focus();
  }

  function showTerminal() {
    authOverlay.style.display = 'none';
    termContainer.style.display = '';
    shellToggle.style.display = '';
  }

  function authenticateShell() {
    var pw = shellPwInput.value;
    if (!pw) return;
    shellAuthBtn.disabled = true;
    shellAuthBtn.textContent = '...';
    var token = getToken();
    fetch('/api/shell-auth?token=' + encodeURIComponent(token), {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({password: pw})
    })
    .then(function(res) {
      if (!res.ok) throw new Error('auth failed');
      return res.json();
    })
    .then(function(data) {
      setShellToken(data.shell_token);
      shellAuthenticated = true;
      showTerminal();
    })
    .catch(function() {
      shellAuthErr.style.display = '';
      shellPwInput.value = '';
      shellPwInput.focus();
    })
    .finally(function() {
      shellAuthBtn.disabled = false;
      shellAuthBtn.textContent = 'Unlock';
    });
  }

  shellAuthBtn.addEventListener('click', authenticateShell);
  shellPwInput.addEventListener('keydown', function(e) {
    if (e.key === 'Enter') authenticateShell();
  });

  function connectShell() {
    if (connected) {
      disconnectShell();
      return;
    }

    if (!shellAuthenticated || !getShellToken()) {
      showAuthOverlay();
      return;
    }

    var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    var url = proto + '//' + location.host + '/ws/shell?token=' + encodeURIComponent(getToken()) +
      '&shell_token=' + encodeURIComponent(getShellToken());

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
      term.onResize(function() {
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

    ws.onclose = function(evt) {
      if (term && connected) {
        term.write('\r\n\x1b[33m[Disconnected]\x1b[0m\r\n');
      }
      connected = false;
      shellToggle.textContent = 'Connect';
      shellToggle.classList.remove('active');
      // If closed due to auth failure (code 1008 or HTTP 401), clear shell token
      if (evt.code === 1008 || evt.code === 4001) {
        sessionStorage.removeItem('sysmon_shell_token');
        shellAuthenticated = false;
        showAuthOverlay();
      }
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
