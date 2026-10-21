/* 录屏工坊 前端 */
const $ = (id) => document.getElementById(id);

const state = {
  mode: 'screen',
  screens: [],
  windows: [],
  recording: null,
  curId: null,
  cur: null,
  timer: null,
  pollTimer: null,
  overlay: false,
};

/* 设置缓存（授权状态、会议提醒开关） */
let settingsCache = { consentAccepted: true, meetingPrompt: true };

/** 弹出授权确认，返回 Promise<boolean> */
function askConsent() {
  return new Promise((resolve) => {
    const box = $('consent');
    box.hidden = false;
    const done = (ok) => {
      box.hidden = true;
      $('consentOk').onclick = null;
      $('consentCancel').onclick = null;
      resolve(ok);
    };
    $('consentOk').onclick = () => done(true);
    $('consentCancel').onclick = () => done(false);
  });
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

async function api(path, opts = {}) {
  const resp = await fetch(path, opts);
  let data = null;
  try { data = await resp.json(); } catch (e) {}
  if (!resp.ok) throw new Error((data && data.error) || resp.statusText);
  return data;
}

function toast(msg, type = 'ok') {
  const t = $('toast');
  t.textContent = msg;
  t.className = 'show ' + type;
  clearTimeout(t._t);
  t._t = setTimeout(() => (t.className = ''), 3000);
}

function fmtDur(ms) {
  if (!ms) return '00:00';
  const s = Math.floor(ms / 1000);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const ss = s % 60;
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`
               : `${String(m).padStart(2, '0')}:${String(ss).padStart(2, '0')}`;
}

function fmtSize(b) {
  if (!b) return '';
  if (b > 1024 ** 3) return (b / 1024 ** 3).toFixed(1) + ' GB';
  if (b > 1024 ** 2) return (b / 1024 ** 2).toFixed(1) + ' MB';
  return Math.round(b / 1024) + ' KB';
}

function fmtClock(sec) {
  const s = Math.floor(sec);
  return `${String(Math.floor(s / 60)).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`;
}

/* ---------- 环境 ---------- */
async function loadState() {
  const s = await api('/api/state');
  state.screens = s.screens || [];
  $('screenSel').innerHTML = state.screens.map(i => `<option value="${i}">显示器 ${i + 1}</option>`).join('');
  const mics = (s.audio || []);
  $('micSel').innerHTML = mics.length
    ? mics.map(d => `<option value="${d.name}">${d.label}</option>`).join('')
    : '<option value="">（未检测到麦克风）</option>';

  const pill = $('pill');
  if (s.ffmpeg) {
    const enc = s.encoder === 'nvenc' ? 'GPU 编码' : 'CPU 编码';
    pill.textContent = `ffmpeg 就绪 · ${enc} · 麦克风 ${mics.length} 个`;
    pill.className = 'pill ok';
  } else {
    pill.textContent = '未找到 ffmpeg';
    pill.className = 'pill bad';
  }
  $('engineInfo').textContent = s.python ? '本地识别引擎：已配置' : '本地识别引擎：缺少 Python';
  if (s.settings) {
    settingsCache = s.settings;
    $('meetingChk').checked = !!s.settings.meetingPrompt;
    $('autoChk').checked = !!s.settings.autoStart;
    $('consentState').textContent = s.settings.consentAccepted ? '已确认' : '未确认';
  }
  if (s.recording && s.recording.status === 'recording') {
    startUI(s.recording);
  }
}

/* ---------- 窗口列表 ---------- */
async function loadWindows() {
  try {
    const list = await api('/api/windows');
    state.windows = list || [];
    const sel = $('winSel');
    if (!state.windows.length) {
      sel.innerHTML = '<option value="0">（没有找到可录制的窗口）</option>';
      return;
    }
    sel.innerHTML = state.windows
      .map(w => `<option value="${w.hwnd}">${escapeHtml(w.title)} · ${w.width}×${w.height}</option>`)
      .join('');
  } catch (e) {
    toast(e.message, 'err');
  }
}

/* ---------- 系统声音设备 ---------- */
async function loadLoopback() {
  try {
    const r = await api('/api/loopback');
    const sel = $('sysSel');
    const cur = sel.value;
    const devs = r.devices || [];
    sel.innerHTML = '<option value="">不录制</option>' +
      '<option value="__process__">全部应用（推荐 · 戴蓝牙耳机也能录）</option>' +
      devs.map(d => {
        const label = d.bluetooth ? `${d.name}（蓝牙·可能录不到）` : d.name;
        return `<option value="${escapeHtml(d.name)}">${escapeHtml(label)}</option>`;
      }).join('');
    const saved = localStorage.getItem('sysDevice') || '';
    if (saved && [...sel.options].some(o => o.value === saved)) sel.value = saved;
    else if (cur && [...sel.options].some(o => o.value === cur)) sel.value = cur;
    sel.onchange = () => {
      localStorage.setItem('sysDevice', sel.value);
      checkSysDevice();
    };
    checkSysDevice();
  } catch (e) {
    $('sysHint').textContent = '无法枚举播放设备：' + e.message;
  }
}

function checkSysDevice() {
  const v = $('sysSel').value;
  const opt = $('sysSel').selectedOptions[0];
  const hint = $('sysHint');
  if (v === '__process__') {
    hint.className = 'muted small';
    hint.textContent = '录下电脑里所有应用的音频，和输出设备无关——戴着蓝牙耳机也能录到对方的声音。停止后会与麦克风混音。';
    return;
  }
  const bt = v && opt && opt.textContent.includes('蓝牙');
  hint.className = bt ? 'warn small' : 'muted small';
  hint.textContent = bt
    ? '⚠️ 这是蓝牙设备，设备级回环拿不到声音。改选上面的「全部应用」，或把系统输出切到扬声器。'
    : '开会时选上它，对方的声音才能录进来。设备级回环只对内置扬声器/有线设备有效，蓝牙请用「全部应用」。';
}

/* ---------- 麦克风检测 ---------- */
function setMeter(el, level) {
  el.style.width = Math.max(0, Math.min(100, level)) + '%';
  el.className = level > 85 ? 'hot' : level > 60 ? 'mid' : '';
}

async function micTest() {
  const mic = $('micSel').value;
  if (!mic) return toast('请先选择麦克风设备', 'err');
  const btn = $('micTestBtn');
  btn.disabled = true;
  btn.textContent = '检测中…';
  $('micAdvice').textContent = '请对着麦克风正常说话（持续 1 秒）';
  try {
    const r = await api('/api/mic/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mic }),
    });
    setMeter($('micFill'), r.level);
    $('micAdvice').textContent = `${r.advice}（峰值 ${r.peakDb} dB，均值 ${r.rmsDb} dB）`;
    $('micAdvice').style.color = r.ok ? '' : 'var(--warn)';
  } catch (e) {
    $('micAdvice').textContent = e.message;
    $('micAdvice').style.color = 'var(--danger)';
  } finally {
    btn.disabled = false;
    btn.textContent = '🎤 检测麦克风电平';
  }
}

/* ---------- 录制 ---------- */
async function startRecording() {
  const mic = $('micSel').value;
  if (!mic) return toast('请先选择麦克风设备', 'err');

  // 首次录音前先做授权确认
  if (!settingsCache.consentAccepted) {
    const ok = await askConsent();
    if (!ok) return;
    try {
      settingsCache = await api('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ acceptConsent: true }),
      });
      $('consentState').textContent = '已确认';
    } catch (e) {
      return toast(e.message, 'err');
    }
  }

  const body = {
    mode: state.mode,
    screen: parseInt($('screenSel').value || '0', 10),
    mic,
    fps: parseInt($('fpsSel').value, 10),
    crf: parseInt($('crfSel').value, 10),
    gainDb: parseInt($('gainSel').value || '0', 10),
    hwnd: state.mode === 'window' ? Number($('winSel').value || 0) : 0,
    segmentMin: parseInt($('segSel').value || '0', 10),
    systemAudio: !!$('sysSel').value,
    sysDevice: $('sysSel').value || '',
  };
  try {
    const rec = await api('/api/record/start', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    startUI(rec);
    toast('开始录制');
  } catch (e) {
    toast(e.message, 'err');
  }
}

function startUI(rec) {
  state.recording = rec;
  $('recBtn').textContent = '■ 停止录制';
  $('recBtn').classList.add('stop');
  $('liveBox').hidden = false;
  $('liveText').textContent = '';
  $('liveState').textContent = '实时转写中…';
  const started = new Date(rec.startedAt).getTime();
  clearInterval(state.timer);
  state.timer = setInterval(() => {
    $('timer').textContent = fmtDur(Date.now() - started);
  }, 500);
  clearInterval(state.pollTimer);
  state.pollTimer = setInterval(pollLive, 1200);
  pollLive();
}

async function stopRecording() {
  try {
    await api('/api/record/stop', { method: 'POST' });
  } catch (e) {
    toast(e.message, 'err');
  }
  clearInterval(state.timer);
  clearInterval(state.pollTimer);
  state.recording = null;
  $('recBtn').textContent = '● 开始录制';
  $('recBtn').classList.remove('stop');
  $('liveState').textContent = '正在整段精校转写…';
  $('liveFill').style.width = '0';
  const id = state.curId;
  setTimeout(async () => {
    await loadList();
    if (id) selectRec(id);
    $('liveBox').hidden = true;
  }, 800);
}

async function pollLive() {
  try {
    const l = await api('/api/live');
    if (!l.recording) return;
    const done = l.text || '';
    const partial = l.partial || '';
    const box = $('liveText');
    box.innerHTML = escapeHtml(done) + (partial ? `<span class="partial">${escapeHtml(partial)}</span>` : '');
    if (!done && !partial) box.textContent = '（等待语音…）';
    box.scrollTop = box.scrollHeight;
    setMeter($('liveFill'), l.level || 0);
    if ((l.level || 0) < 3) {
      $('liveState').textContent = '⚠️ 没有检测到声音，检查麦克风';
    } else if (l.segments.length || partial) {
      $('liveState').textContent = `已转写 ${l.segments.length} 段${partial ? ' · 识别中' : ''}`;
    } else {
      $('liveState').textContent = '正在听…';
    }
  } catch (e) { /* 忽略轮询错误 */ }
}

/* ---------- 记录列表 ---------- */
async function loadList() {
  const list = await api('/api/recordings');
  $('count').textContent = list.length ? `${list.length} 条` : '';
  const ul = $('recList');
  ul.innerHTML = '';
  if (!list.length) {
    $('empty').hidden = false;
    $('detail').hidden = true;
    return;
  }
  $('empty').hidden = true;
  for (const r of list) {
    const li = document.createElement('li');
    li.className = 'rec-item' + (r.id === state.curId ? ' active' : '');
    const left = document.createElement('div');
    left.style.minWidth = '0';
    const name = document.createElement('div');
    name.className = 'r-name';
    name.textContent = r.title;
    const meta = document.createElement('div');
    meta.className = 'r-meta';
    meta.textContent = `${r.mode === 'audio' ? '录音' : '录屏'} · ${fmtDur(r.durationMs)}${r.sizeBytes ? ' · ' + fmtSize(r.sizeBytes) : ''}`;
    left.appendChild(name);
    left.appendChild(meta);

    const tag = document.createElement('span');
    if (r.transcribing) { tag.className = 'r-tag busy'; tag.textContent = '转写中'; }
    else if (r.segments && r.segments.length) { tag.className = 'r-tag ok'; tag.textContent = '已转写'; }
    else { tag.className = 'r-tag'; tag.textContent = '无文字'; }

    li.appendChild(left);
    li.appendChild(tag);
    li.onclick = () => selectRec(r.id);
    ul.appendChild(li);
  }
}

function updateExportLinks(id) {
  const q = $('mathChk').checked ? '&math=1' : '';
  $('mdBtn').href = `/api/recordings/${id}/export?format=md${q}`;
  $('docxBtn').href = `/api/recordings/${id}/export?format=docx${q}`;
}

async function selectRec(id) {
  state.curId = id;
  await loadList();
  const r = await api('/api/recordings/' + id);
  state.cur = r;
  $('empty').hidden = true;
  $('detail').hidden = false;
  $('dTitle').textContent = r.title;
  $('dMeta').textContent = `${new Date(r.startedAt).toLocaleString('zh-CN', { hour12: false })} · ${fmtDur(r.durationMs)} · ${fmtSize(r.sizeBytes)}${r.mic ? ' · ' + r.mic : ''}${r.systemAudio ? ' · 含系统声音' : ''}`;
  if (r.sysWarning) toast(r.sysWarning, 'err');
  $('player').src = '/api/recordings/' + id + '/media';
  renderParts(r, id);
  $('transBtn').textContent = r.translateDir ? '重新翻译' : '翻译字幕';
  $('txtBtn').href = '/api/recordings/' + id + '/transcript?format=txt';
  $('srtBtn').href = '/api/recordings/' + id + '/transcript?format=srt';
  updateExportLinks(id);
  renderTranscript(r);
  // 转写中/分离中则轮询刷新
  if (r.transcribing || r.diarizing) {
    setTimeout(() => { if (state.curId === id) selectRec(id); }, 2000);
  }
}

function renderParts(r, id) {
  const parts = r.parts || [];
  const row = $('partRow');
  const box = $('partBtns');
  box.innerHTML = '';
  if (parts.length <= 1) {
    row.hidden = true;
    return;
  }
  row.hidden = false;
  parts.forEach((p, i) => {
    const b = document.createElement('button');
    b.className = 'btn small' + (i === 0 ? ' active-part' : '');
    b.textContent = `第 ${i + 1} 段`;
    b.onclick = () => {
      $('player').src = '/api/recordings/' + id + '/media?part=' + i;
      Array.from(box.children).forEach(c => c.classList.remove('active-part'));
      b.classList.add('active-part');
    };
    box.appendChild(b);
  });
}

function renderTranscript(r) {
  const box = $('segments');
  const plain = $('plainText');
  box.innerHTML = '';
  if (r.diarizing) {
    $('transcribeState').textContent = '⏳ 正在区分说话人（首次加载模型较慢）…';
  } else if (r.transcribing) {
    $('transcribeState').textContent = '⏳ 正在整段转写（比实时更准）…';
  } else if (r.error) {
    $('transcribeState').textContent = '⚠️ ' + r.error;
  } else if (r.segments && r.segments.length) {
    const spk = r.diarized && r.speakers ? ` · 已区分 ${r.speakers} 个说话人` : '';
    $('transcribeState').textContent = `共 ${r.segments.length} 段 · ${r.transcribed ? '已精校' : '实时结果'}${spk}`;
  } else if (r.transcribed) {
    $('transcribeState').textContent = '没有识别到语音：可能是麦克风静音或录音时无人说话。点左侧「检测麦克风电平」排查。';
  } else {
    $('transcribeState').textContent = '没有识别到语音';
  }
  plain.hidden = true;

  (r.segments || []).forEach(s => {
    const div = document.createElement('div');
    div.className = 'seg';
    const t = document.createElement('span');
    t.className = 'seg-time';
    t.textContent = fmtClock(s.start);
    div.appendChild(t);
    if (s.speaker) {
      const sp = document.createElement('span');
      sp.className = 'speaker spk' + (((s.speaker - 1) % 4) + 1);
      sp.textContent = '说话人 ' + s.speaker;
      div.appendChild(sp);
    }
    const txt = document.createElement('span');
    txt.className = 'seg-text';
    txt.textContent = s.text;
    div.appendChild(txt);
    if (s.trans) {
      const tr = document.createElement('div');
      tr.className = 'seg-trans';
      tr.textContent = s.trans;
      div.appendChild(tr);
    }
    div.onclick = () => {
      const p = $('player');
      p.currentTime = Math.max(0, s.start - 0.3);
      p.play();
    };
    box.appendChild(div);
  });
}

/* ---------- 操作 ---------- */
async function rename() {
  if (!state.cur) return;
  const name = prompt('新的名称', state.cur.title);
  if (!name) return;
  try {
    await api('/api/recordings/' + state.cur.id, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title: name }),
    });
    await selectRec(state.cur.id);
    toast('已改名');
  } catch (e) { toast(e.message, 'err'); }
}

async function retranscribe() {
  if (!state.cur) return;
  try {
    await api('/api/recordings/' + state.cur.id + '/transcribe', { method: 'POST' });
    toast('已开始转写');
    selectRec(state.cur.id);
  } catch (e) { toast(e.message, 'err'); }
}

async function diarize() {
  if (!state.cur) return;
  try {
    await api('/api/recordings/' + state.cur.id + '/diarize', { method: 'POST' });
    toast('已开始区分说话人');
    selectRec(state.cur.id);
  } catch (e) { toast(e.message, 'err'); }
}

async function translateSegs() {
  if (!state.cur) return;
  const btn = $('transBtn');
  const old = btn.textContent;
  btn.disabled = true;
  btn.textContent = '翻译中…';
  $('transcribeState').textContent = '⏳ 正在本地翻译（首次加载模型约 10 秒）…';
  try {
    const res = await api('/api/recordings/' + state.cur.id + '/translate', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ dir: 'auto' }),
    });
    toast(`已翻译 ${res.count} 句（${res.dir === 'zh2en' ? '中 → 英' : '英 → 中'}）`);
    await selectRec(state.cur.id);
  } catch (e) {
    toast('翻译失败：' + e.message, 'err');
    $('transcribeState').textContent = '⚠️ ' + e.message;
  } finally {
    btn.disabled = false;
    btn.textContent = old;
  }
}

async function del() {
  if (!state.cur) return;
  if (!confirm('确定删除这条记录？文件会一并删除。')) return;
  try {
    await api('/api/recordings/' + state.cur.id, { method: 'DELETE' });
    state.cur = null;
    state.curId = null;
    $('detail').hidden = true;
    await loadList();
    toast('已删除');
  } catch (e) { toast(e.message, 'err'); }
}

/* ---------- 初始化 ---------- */
document.addEventListener('DOMContentLoaded', async () => {
  try {
    await loadState();
    loadLoopback(); // 枚举播放设备要起 python，放后台不阻塞首屏
    await loadList();
    if (!state.curId) {
      const list = await api('/api/recordings');
      if (list.length) selectRec(list[0].id);
    }
  } catch (e) {
    toast('初始化失败: ' + e.message, 'err');
  }

  document.querySelectorAll('.tab').forEach(tab => {
    tab.onclick = () => {
      document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
      tab.classList.add('active');
      state.mode = tab.dataset.mode;
      const isAudio = state.mode === 'audio';
      $('screenRow').hidden = state.mode !== 'screen';
      $('windowRow').hidden = state.mode !== 'window';
      $('fpsRow').hidden = isAudio;
      $('crfRow').hidden = isAudio;
      $('segRow').hidden = isAudio;
      if (state.mode === 'window' && !state.windows.length) loadWindows();
    };
  });

  $('recBtn').onclick = () => {
    if (state.recording) stopRecording();
    else startRecording();
  };
  $('micTestBtn').onclick = micTest;
  $('winRefresh').onclick = loadWindows;
  $('mathChk').onchange = () => { if (state.curId) updateExportLinks(state.curId); };
  $('meetingChk').onchange = async (e) => {
    try {
      settingsCache = await api('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ meetingPrompt: e.target.checked }),
      });
      toast(e.target.checked ? '开会时会自动提醒' : '已关闭会议提醒');
    } catch (err) { toast(err.message, 'err'); }
  };
  $('autoChk').onchange = async (e) => {
    const on = e.target.checked;
    try {
      settingsCache = await api('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ autoStart: on }),
      });
      e.target.checked = !!settingsCache.autoStart;
      toast(on ? '已设为开机自动启动' : '已取消开机自启');
    } catch (err) {
      e.target.checked = !on;
      toast(err.message, 'err');
    }
  };
  $('resetOverlayPos').onclick = async (e) => {
    e.preventDefault();
    try {
      await api('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resetOverlayPos: true }),
      });
      toast('悬浮条已复位到屏幕底部');
    } catch (err) { toast(err.message, 'err'); }
  };
  $('resetConsent').onclick = async (e) => {
    e.preventDefault();
    try {
      settingsCache = await api('/api/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ resetConsent: true }),
      });
      $('consentState').textContent = '未确认';
      toast('已重置，下次录音前会重新要求确认');
    } catch (err) { toast(err.message, 'err'); }
  };
  $('overlayBtn').onclick = async () => {
    state.overlay = !state.overlay;
    try {
      await api('/api/overlay', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ show: state.overlay }),
      });
      toast(state.overlay ? '悬浮字幕已显示（置顶、鼠标可穿透）' : '悬浮字幕已隐藏');
    } catch (e) { toast(e.message, 'err'); }
  };
  $('micSel').onchange = () => {
    $('micFill').style.width = '0';
    $('micAdvice').textContent = '';
  };
  $('renameBtn').onclick = rename;
  $('transcribeBtn').onclick = retranscribe;
  $('diarizeBtn').onclick = diarize;
  $('transBtn').onclick = translateSegs;
  $('delBtn').onclick = del;
});
