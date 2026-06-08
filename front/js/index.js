/* ==================================================================
   WPS 灵犀 — 三视图前端 (chat / templates / editor)
   ================================================================== */

const STORAGE_KEYS = {
  userId: 'lingxi-user-id',
  authToken: 'lingxi-auth-token',
  authUsername: 'lingxi-auth-username',
  activeView: 'lingxi-active-view',
  activeSessionId: 'lingxi-active-session-id',
  pendingStream: 'lingxi-pending-stream',
  sidebarCollapsed: 'lingxi-sidebar-collapsed',
};

const API_BASE = (() => {
  const { port } = window.location;
  if (port && !['3000', '5173', '5174'].includes(port)) return `${window.location.origin}/api`;
  return `${window.location.protocol}//${window.location.hostname}:8080/api`;
})();
const API_ORIGIN = API_BASE.replace(/\/api$/, '');
const PAGE_PARAMS = new URLSearchParams(window.location.search);
const INITIAL_AUTH_TOKEN = PAGE_PARAMS.get('auth_token') || localStorage.getItem(STORAGE_KEYS.authToken) || '';
if (PAGE_PARAMS.get('auth_token')) {
  localStorage.setItem(STORAGE_KEYS.authToken, PAGE_PARAMS.get('auth_token'));
}

const CONFIG = {
  maxFileCount: 8,
  maxTextCharsPerFile: 12000,
  pendingStatePersistDebounceMs: 500,
  defaultSuggestions: [
    '补充关键章节的数据支撑',
    '增加结论与行动建议部分',
    '优化标题措辞，更具吸引力',
  ],
};

const state = {
  userId: localStorage.getItem(STORAGE_KEYS.userId) || '',
  authToken: INITIAL_AUTH_TOKEN,
  username: localStorage.getItem(STORAGE_KEYS.authUsername) || '',
  authMode: 'login',
  activeView: localStorage.getItem(STORAGE_KEYS.activeView) || 'chat',
  sessions: [],
  activeSessionId: localStorage.getItem(STORAGE_KEYS.activeSessionId) || null,
  activeMessageId: '',
  parentMessageId: '',
  openSessionMenuId: null,
  sessionSearchQuery: '',
  messages: [],
  uploads: [],
  abortController: null,
  isStreaming: false,
  pptProject: null,
  activeSlideIndex: 1,
  hasAutoSwitchedToEditor: false,
  pptTemplates: [],
  selectedTemplateName: '',
  pendingTemplateRequest: null,
  templateRecommendation: null,
  awaitingTemplateDecision: false,
  sidebarCollapsed: localStorage.getItem(STORAGE_KEYS.sidebarCollapsed) === 'true',
  // 新流水线（v2）状态。后端 SSE 每条 payload.stage 推动这里更新；为空表示走旧 SKILL.md 模式。
  pipelineStage: '',
  pipelinePageOptions: [],
  pipelineOutline: null,
  pipelineAwaitingPagePick: false,
  pipelineAwaitingOutlineConfirm: false,
};

if (state.activeView === 'editor') {
  state.activeView = 'chat';
  localStorage.setItem(STORAGE_KEYS.activeView, 'chat');
}

if (state.userId) localStorage.setItem(STORAGE_KEYS.userId, state.userId);

let pendingStatePersistTimer = null;

const elements = {
  body: document.body,
  views: {
    chat: document.querySelector('.view-chat'),
    templates: document.querySelector('.view-templates'),
  },
  viewTabs: document.querySelectorAll('.view-tab'),

  // chat
  bodyRow: document.querySelector('.view-chat .body-row'),
  chatSidebar: document.getElementById('chatSidebar'),
  sidebarToggleBtn: document.getElementById('sidebarToggleBtn'),
  sidebarExpandBtn: document.getElementById('sidebarExpandBtn'),
  chatHeaderTitle: document.getElementById('chatHeaderTitle'),
  chatUserName: document.getElementById('chatUserName'),
  logoutBtn: document.getElementById('logoutBtn'),
  chatScrollArea: document.getElementById('chatScrollArea'),
  chatContainer: document.getElementById('chatContainer'),
  heroSection: document.getElementById('heroSection'),
  suggestionGrid: document.getElementById('suggestionGrid'),
  sessionList: document.getElementById('sessionList'),
  sessionSearchInput: document.getElementById('sessionSearchInput'),
  newSessionBtn: document.getElementById('newSessionBtn'),
  messageInput: document.getElementById('messageInput'),
  sendBtn: document.getElementById('sendBtn'),
  stopBtn: document.getElementById('stopBtn'),
  clearChatBtn: document.getElementById('clearChatBtn'),
  imageInput: document.getElementById('imageInput'),
  uploadPreview: document.getElementById('uploadPreview'),

  // templates
  tplGenerateBtn: document.getElementById('tplGenerateBtn'),
  tplGenerateBtnLabel: document.getElementById('tplGenerateBtnLabel'),
  tplRecommendationBanner: document.getElementById('tplRecommendationBanner'),
  tplRecommendationTitle: document.getElementById('tplRecommendationTitle'),
  tplRecommendationReason: document.getElementById('tplRecommendationReason'),
  tplBackToChatBtn: document.getElementById('tplBackToChatBtn'),
  tplPreviewLarge: document.querySelector('.tpl-preview-large'),
  tplPreviewGrid: document.querySelector('.tpl-preview-grid'),
  tplGrid: document.querySelector('.tpl-grid'),
  tplFilterTabs: document.querySelectorAll('.tpl-filter-tab'),

  // pipeline v2 progress / cards
  pipelineBar: document.getElementById('pipelineBar'),
  pipelineCards: document.getElementById('pipelineCards'),

  // shared
  rightRails: document.querySelectorAll('.right-rail'),
  rightRailTemplate: document.getElementById('rightRailTemplate'),
  messageTemplate: document.getElementById('messageTemplate'),

  // modals
  imagePreviewModal: document.getElementById('imagePreviewModal'),
  imagePreviewLarge: document.getElementById('imagePreviewLarge'),
  imagePreviewClose: document.getElementById('imagePreviewClose'),
  slidePreviewModal: document.getElementById('slidePreviewModal'),
  slidePreviewContent: document.getElementById('slidePreviewContent'),
  slidePreviewClose: document.getElementById('slidePreviewClose'),

  // auth
  authOverlay: document.getElementById('authOverlay'),
  authForm: document.getElementById('authForm'),
  authTitle: document.getElementById('authTitle'),
  authError: document.getElementById('authError'),
  authUsername: document.getElementById('authUsername'),
  authPassword: document.getElementById('authPassword'),
  authSubmitBtn: document.getElementById('authSubmitBtn'),
  authModeBtn: document.getElementById('authModeBtn'),
};

/* ============================ utils ============================ */

function escapeHtml(str = '') {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function getFileExtension(name = '') {
  const parts = String(name).toLowerCase().split('.');
  return parts.length > 1 ? parts.pop() : '';
}

function getPPTistEditorURL(projectName = '') {
  const params = new URLSearchParams();
  if (projectName) params.set('project', projectName);
  if (state.authToken) params.set('auth_token', state.authToken);
  return `${API_ORIGIN}/pptist/index.html${params.toString() ? `?${params.toString()}` : ''}`;
}

function getStandaloneEditorURL(projectName = '') {
  const params = new URLSearchParams();
  if (projectName) params.set('project', projectName);
  if (state.authToken) params.set('auth_token', state.authToken);
  return `${API_ORIGIN}/editor${params.toString() ? `?${params.toString()}` : ''}`;
}

function withAuthURL(url = '') {
  if (!url || !state.authToken) return url;
  try {
    const base = /^https?:\/\//i.test(url) ? undefined : API_ORIGIN;
    const parsed = new URL(url, base);
    if (parsed.pathname.startsWith('/api/projects/') || parsed.pathname.startsWith('/pptist/')) {
      parsed.searchParams.set('auth_token', state.authToken);
    }
    return parsed.toString();
  } catch {
    return url;
  }
}

function getProjectNameFromURL() {
  return PAGE_PARAMS.get('project') || '';
}

function openStandaloneEditor(projectName = '') {
  window.location.href = getStandaloneEditorURL(projectName || state.pptProject?.name || '');
}

function openStandaloneEditorInNewTab(projectName = '') {
  window.open(getStandaloneEditorURL(projectName || state.pptProject?.name || ''), '_blank', 'noopener');
}

function detectFileKind(fileLike = {}) {
  const mimeType = (fileLike.type || fileLike.mimeType || '').toLowerCase();
  const ext = getFileExtension(fileLike.name || '');
  if (mimeType.startsWith('image/') || ['png', 'jpg', 'jpeg', 'webp', 'gif', 'bmp', 'svg'].includes(ext)) return 'image';
  if (
    mimeType.startsWith('text/')
    || ['txt', 'md', 'markdown', 'json', 'js', 'ts', 'tsx', 'jsx', 'css', 'html', 'htm', 'xml', 'csv', 'yaml', 'yml', 'log'].includes(ext)
    || ['application/json', 'application/xml'].includes(mimeType)
  ) return 'text';
  if (mimeType === 'application/pdf' || ext === 'pdf') return 'pdf';
  if (mimeType === 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' || ext === 'docx') return 'docx';
  return 'other';
}

function formatFileSize(size = 0) {
  if (!size) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let value = size;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex += 1;
  }
  return `${value >= 10 || unitIndex === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unitIndex]}`;
}

function getFileBadgeLabel(upload) {
  const ext = getFileExtension(upload.name || '');
  if (upload.kind === 'image') return 'IMG';
  if (upload.kind === 'pdf') return 'PDF';
  if (upload.kind === 'docx') return 'DOCX';
  if (upload.kind === 'text') return ext ? ext.toUpperCase().slice(0, 4) : 'TXT';
  return ext ? ext.toUpperCase().slice(0, 5) : 'FILE';
}

function fileToBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result);
    reader.onerror = reject;
    reader.readAsDataURL(file);
  });
}

function readTextFile(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result || ''));
    reader.onerror = reject;
    reader.readAsText(file);
  });
}

async function extractPdfText(file) {
  if (!window.pdfjsLib) throw new Error('pdfjs unavailable');
  if (!window.pdfjsLib.GlobalWorkerOptions.workerSrc) {
    window.pdfjsLib.GlobalWorkerOptions.workerSrc = 'https://cdnjs.cloudflare.com/ajax/libs/pdf.js/3.11.174/pdf.worker.min.js';
  }
  const buffer = await file.arrayBuffer();
  const pdf = await window.pdfjsLib.getDocument({ data: buffer }).promise;
  const pages = [];
  for (let pageNum = 1; pageNum <= pdf.numPages; pageNum += 1) {
    const page = await pdf.getPage(pageNum);
    const content = await page.getTextContent();
    pages.push(content.items.map((item) => item.str).join(' '));
  }
  return pages.join('\n').trim();
}

async function extractDocxText(file) {
  if (!window.mammoth) throw new Error('mammoth unavailable');
  const arrayBuffer = await file.arrayBuffer();
  const result = await window.mammoth.extractRawText({ arrayBuffer });
  return String(result.value || '').trim();
}

function clampExtractedText(text = '') {
  return String(text).slice(0, CONFIG.maxTextCharsPerFile);
}

async function parseUploadFile(file) {
  const kind = detectFileKind(file);
  const upload = {
    id: crypto.randomUUID(),
    name: file.name,
    size: file.size || 0,
    mimeType: file.type || 'application/octet-stream',
    kind,
    url: '',
    data: '',
    textContent: '',
    parseStatus: 'fallback',
  };

  try {
    if (kind === 'image') {
      upload.url = URL.createObjectURL(file);
      upload.data = await fileToBase64(file);
      upload.parseStatus = 'success';
      return upload;
    }
    if (kind === 'text') {
      upload.textContent = clampExtractedText(await readTextFile(file));
      upload.parseStatus = upload.textContent ? 'success' : 'fallback';
      return upload;
    }
    if (kind === 'pdf') {
      upload.textContent = clampExtractedText(await extractPdfText(file));
      upload.parseStatus = upload.textContent ? 'success' : 'fallback';
      return upload;
    }
    if (kind === 'docx') {
      upload.textContent = clampExtractedText(await extractDocxText(file));
      upload.parseStatus = upload.textContent ? 'success' : 'fallback';
      return upload;
    }
  } catch (error) {
    console.warn('文件解析失败', file.name, error);
    upload.parseStatus = 'failed';
  }
  return upload;
}

function normalizeUploads(uploads = []) {
  return uploads.map((item) => ({
    id: item.id || crypto.randomUUID(),
    name: item.name || 'file',
    size: item.size || 0,
    mimeType: item.mimeType || item.type || 'application/octet-stream',
    kind: item.kind || detectFileKind(item),
    data: item.data || '',
    url: item.url || '',
    textContent: item.textContent || '',
    parseStatus: item.parseStatus || 'fallback',
  }));
}

function persistActiveSessionId() {
  if (state.activeSessionId) {
    localStorage.setItem(STORAGE_KEYS.activeSessionId, state.activeSessionId);
  } else {
    localStorage.removeItem(STORAGE_KEYS.activeSessionId);
  }
}

function serializeUploadsForStorage(uploads = []) {
  return normalizeUploads(uploads).map((item) => ({
    id: item.id,
    name: item.name,
    size: item.size,
    mimeType: item.mimeType,
    kind: item.kind,
    data: item.data || '',
    url: item.data || item.url || '',
    textContent: item.textContent || '',
    parseStatus: item.parseStatus || 'fallback',
  }));
}

function serializeMessageForStorage(record) {
  if (!record) return null;
  return {
    id: record.id || '',
    role: record.role || '',
    content: record.content || '',
    uploads: serializeUploadsForStorage(record.uploads || []),
    tools: (record.tools || []).map((tool) => ({
      id: tool.id || '',
      toolName: tool.toolName || '',
      arguments: tool.arguments || '',
      result: tool.result || '',
    })),
    pptSnapshot: record.pptSnapshot || null,
  };
}

function serializePPTProjectForStorage(ppt) {
  if (!ppt) return null;
  const pages = (ppt.pages || []).filter(Boolean);
  return {
    name: ppt.name || '',
    path: ppt.path || '',
    exportUrl: ppt.exportUrl || '',
    fileName: ppt.fileName || '',
    pageCount: pages.length,
    pages: pages.map((page) => ({
      pageIndex: page.pageIndex,
      fileName: page.fileName || '',
    })),
  };
}

function getPendingStreamState() {
  if (!state.isStreaming || !state.activeSessionId) return null;
  let assistantRecord = null;
  let userRecord = null;
  for (let i = state.messages.length - 1; i >= 0; i -= 1) {
    const record = state.messages[i];
    if (!assistantRecord && record.role === 'assistant') {
      assistantRecord = record;
      continue;
    }
    if (assistantRecord && record.role === 'user') {
      userRecord = record;
      break;
    }
  }
  if (!assistantRecord) return null;
  return {
    conversationId: state.activeSessionId,
    activeMessageId: state.activeMessageId || assistantRecord.id || '',
    activeView: state.activeView,
    activeSlideIndex: state.activeSlideIndex,
    savedAt: Date.now(),
    userMessage: serializeMessageForStorage(userRecord),
    assistantMessage: serializeMessageForStorage(assistantRecord),
    pptProject: serializePPTProjectForStorage(state.pptProject),
  };
}

function persistPendingStreamStateNow() {
  try {
    const pending = getPendingStreamState();
    if (!pending) {
      sessionStorage.removeItem(STORAGE_KEYS.pendingStream);
      return;
    }
    sessionStorage.setItem(STORAGE_KEYS.pendingStream, JSON.stringify(pending));
  } catch (error) {
    console.warn('persist pending stream failed', error);
  }
}

function persistPendingStreamState(immediate = false) {
  if (pendingStatePersistTimer) {
    clearTimeout(pendingStatePersistTimer);
    pendingStatePersistTimer = null;
  }
  if (immediate) {
    persistPendingStreamStateNow();
    return;
  }
  pendingStatePersistTimer = setTimeout(() => {
    pendingStatePersistTimer = null;
    persistPendingStreamStateNow();
  }, CONFIG.pendingStatePersistDebounceMs);
}

function clearPendingStreamState() {
  if (pendingStatePersistTimer) {
    clearTimeout(pendingStatePersistTimer);
    pendingStatePersistTimer = null;
  }
  sessionStorage.removeItem(STORAGE_KEYS.pendingStream);
}

function restorePendingStreamState(historyMessages = []) {
  let pending = null;
  try {
    const raw = sessionStorage.getItem(STORAGE_KEYS.pendingStream);
    if (!raw) return false;
    pending = JSON.parse(raw);
  } catch (error) {
    console.warn('restore pending stream failed', error);
    clearPendingStreamState();
    return false;
  }

  if (!pending || pending.conversationId !== state.activeSessionId) return false;
  const persistedMessage = pending.activeMessageId
    ? historyMessages.find((message) => message.message_id === pending.activeMessageId)
    : null;
  const persistedMessageHasResponse = !!String(persistedMessage?.response || '').trim();
  if (persistedMessageHasResponse) {
    clearPendingStreamState();
    return false;
  }

  const restoredUser = pending.userMessage;
  const restoredAssistant = pending.assistantMessage;

  const hasPersistedShell = !!persistedMessage;

  if (!hasPersistedShell && restoredUser && (restoredUser.content || (restoredUser.uploads || []).length > 0)) {
    appendMessage('user', restoredUser.content || '', {
      id: restoredUser.id,
      uploads: restoredUser.uploads || [],
    });
  }
  if (!hasPersistedShell && restoredAssistant) {
    appendMessage('assistant', restoredAssistant.content || '', {
      id: restoredAssistant.id,
      tools: restoredAssistant.tools || [],
      pptSnapshot: restoredAssistant.pptSnapshot || null,
    });
  }

  if (pending.pptProject) {
    const restoredPages = (pending.pptProject.pages || [])
      .filter(Boolean)
      .sort((a, b) => (a.pageIndex || 0) - (b.pageIndex || 0))
      .map((page) => ({
        pageIndex: page.pageIndex,
        fileName: page.fileName || '',
        svgContent: '',
      }));
    const fallbackPageCount = Number(pending.pptProject.pageCount) || restoredPages.length;
    state.pptProject = {
      name: pending.pptProject.name || '',
      path: pending.pptProject.path || '',
      exportUrl: pending.pptProject.exportUrl || '',
      fileName: pending.pptProject.fileName || '',
      pages: restoredPages.length > 0
        ? restoredPages
        : Array.from({ length: Math.max(fallbackPageCount, 0) }, (_, index) => ({
          pageIndex: index + 1,
          fileName: '',
          svgContent: '',
        })),
    };
    state.activeSlideIndex = Number(pending.activeSlideIndex) || 1;
    renderRightRails();
    if (state.pptProject.name) {
      restoreProjectFromURL(state.pptProject.name).catch((error) => {
        console.warn('restore pending PPT project failed', error);
      });
      upsertProjectCardMessage(state.pptProject);
    }
  }

  hideHeroIfNeeded();
  showToast('页面已恢复未完成的回复；若生成被中断，请重新点击生成。', 'info');
  return true;
}

function showToast(message, type = 'info') {
  const existing = document.querySelector('.toast-notification');
  if (existing) existing.remove();
  const toast = document.createElement('div');
  toast.className = `toast-notification toast-${type}`;
  toast.textContent = message;
  document.body.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add('show'));
  setTimeout(() => {
    toast.classList.remove('show');
    setTimeout(() => toast.remove(), 300);
  }, 3000);
}

function updateStatus(text) {
  void text;
}

function autoResizeTextarea() {
  const el = elements.messageInput;
  if (!el) return;
  el.style.height = 'auto';
  el.style.height = `${Math.min(el.scrollHeight, 180)}px`;
}

function setStreamingStatus(isStreaming) {
  state.isStreaming = isStreaming;
  elements.stopBtn?.classList.toggle('hidden', !isStreaming);
  toggleSendBtn();
  updateStatus(isStreaming ? 'Go SSE 流式响应中' : 'Go SSE 已连接');
}

function hideHeroIfNeeded() {
  const hasMessages = state.messages.length > 0;
  elements.heroSection?.classList.toggle('hidden', hasMessages);
}

function scrollChatToBottom() {
  requestAnimationFrame(() => {
    elements.chatScrollArea?.scrollTo({ top: elements.chatScrollArea.scrollHeight, behavior: 'smooth' });
  });
}

/* ============================ view switching ============================ */

function setActiveView(view) {
  if (!elements.views[view]) return;
  state.activeView = view;
  localStorage.setItem(STORAGE_KEYS.activeView, view);
  Object.entries(elements.views).forEach(([key, el]) => {
    el.classList.toggle('active', key === view);
  });
  elements.body.dataset.activeView = view;
  if (view === 'chat') scrollChatToBottom();
}

function setSidebarCollapsed(collapsed) {
  state.sidebarCollapsed = collapsed;
  localStorage.setItem(STORAGE_KEYS.sidebarCollapsed, collapsed ? 'true' : 'false');
  elements.bodyRow?.classList.toggle('sidebar-collapsed', collapsed);
  elements.sidebarToggleBtn?.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
}

/* ============================ markdown / message rendering ============================ */

if (window.marked) marked.setOptions({ breaks: true, gfm: true });
if (window.Prism?.plugins?.autoloader) {
  window.Prism.plugins.autoloader.languages_path = 'https://cdn.jsdelivr.net/npm/prismjs@1.29.0/components/';
}

function detectCodeLanguage(code = '') {
  const text = code.trim();
  if (!text) return 'text';
  if (/^package\s+main/m.test(text) || /\bfunc\s+\w+\s*\(/.test(text)) return 'go';
  if (/\bconsole\.log\b|\bconst\b|\blet\b|=>/.test(text)) return 'javascript';
  if (/^\s*<\/?[a-z][\s\S]*>/i.test(text)) return 'markup';
  if (/^\s*\{[\s\S]*\}\s*$/.test(text) && /"[^"]+"\s*:/.test(text)) return 'json';
  if (/^\s*def\s+\w+\s*\(/m.test(text) || /\bimport\s+[a-zA-Z0-9_]+/.test(text)) return 'python';
  if (/^\s*(SELECT|INSERT|UPDATE|DELETE|CREATE|ALTER)\b/i.test(text)) return 'sql';
  if (/^\s*curl\s+/.test(text) || /^\s*#!/.test(text)) return 'bash';
  return 'text';
}

function renderMarkdown(markdown = '') {
  if (!window.marked) {
    return `<p>${escapeHtml(markdown).replace(/\n/g, '<br />')}</p>`;
  }
  const rawHtml = marked.parse(markdown || '');
  const wrapper = document.createElement('div');
  wrapper.innerHTML = rawHtml;

  wrapper.querySelectorAll('pre').forEach((pre) => {
    const codeEl = pre.querySelector('code');
    if (!codeEl) return;
    const className = codeEl.className || '';
    const matched = className.match(/language-([\w-]+)/);
    const rawCode = codeEl.textContent || '';
    const lang = matched ? matched[1] : detectCodeLanguage(rawCode);
    pre.className = `language-${lang}`;
    codeEl.className = `language-${lang}`;
    codeEl.textContent = rawCode;

    const copyBtn = document.createElement('button');
    copyBtn.className = 'copy-btn';
    copyBtn.type = 'button';
    copyBtn.dataset.copy = encodeURIComponent(rawCode);
    copyBtn.textContent = '复制';

    const wrapperBlock = document.createElement('div');
    wrapperBlock.className = 'code-block';
    const head = document.createElement('div');
    head.className = 'code-block-head';
    const langLabel = document.createElement('span');
    langLabel.className = 'code-lang';
    langLabel.textContent = lang;
    head.appendChild(langLabel);
    head.appendChild(copyBtn);
    wrapperBlock.appendChild(head);
    pre.parentNode.insertBefore(wrapperBlock, pre);
    wrapperBlock.appendChild(pre);

    if (window.Prism?.highlightElement) window.Prism.highlightElement(codeEl);
  });

  wrapper.querySelectorAll('a').forEach((link) => {
    link.setAttribute('target', '_blank');
    link.setAttribute('rel', 'noreferrer noopener');
  });
  return wrapper.innerHTML;
}

function enhanceRenderedMessage(container) {
  container.querySelectorAll('.copy-btn').forEach((btn) => {
    btn.onclick = async () => {
      const text = decodeURIComponent(btn.dataset.copy || '');
      try {
        await navigator.clipboard.writeText(text);
        const old = btn.textContent;
        btn.textContent = '已复制';
        setTimeout(() => { btn.textContent = old; }, 1200);
      } catch {
        btn.textContent = '复制失败';
      }
    };
  });
}

function renderToolBlock(tool) {
  if (!tool) return '';
  const title = tool.toolName || tool.name || 'tool';
  const args = tool.arguments || '';
  const result = tool.result || '';
  const statusLabel = result ? '已完成' : '执行中';
  const statusCls = result ? 'done' : 'running';
  return `
    <details class="tool-block">
      <summary>
        <span class="tool-name">${escapeHtml(title)}</span>
        <span class="tool-status ${statusCls}">${statusLabel}</span>
      </summary>
      <div class="tool-block-body">
        ${args ? `<div class="tool-field"><div class="tool-field-label">参数</div><pre>${escapeHtml(args)}</pre></div>` : ''}
        ${result ? `<div class="tool-field"><div class="tool-field-label">结果</div><pre>${escapeHtml(result)}</pre></div>` : ''}
      </div>
    </details>
  `;
}

function renderUserMessage(text, uploads) {
  const safeUploads = normalizeUploads(uploads);
  const images = safeUploads
    .filter((file) => file.kind === 'image' && (file.url || file.data))
    .map((file) => `<img class="inline-upload" src="${file.url || file.data}" alt="${escapeHtml(file.name)}" />`)
    .join('');
  const files = safeUploads
    .filter((file) => !(file.kind === 'image' && (file.url || file.data)))
    .map((file) => `<div class="user-upload-note">附件：${escapeHtml(file.name)} · ${escapeHtml(formatFileSize(file.size))}</div>`)
    .join('');
  return `${images}${files}<p>${escapeHtml(text || '请结合附件内容进行分析').replace(/\n/g, '<br />')}</p>`;
}

function renderAssistantMessage(record) {
  const contentHtml = renderMarkdown(record.content || '');
  const toolHtml = (record.tools || []).map(renderToolBlock).join('');
  const pptCardHtml = record.pptSnapshot ? renderInlinePPTCard(record.pptSnapshot) : '';
  return `${contentHtml}${toolHtml}${pptCardHtml}`;
}

function renderInlinePPTCard(snapshot) {
  const rawName = snapshot.name || 'PPT 项目';
  const name = escapeHtml(rawName);
  const pages = snapshot.pages || 0;
  const exported = !!snapshot.exportUrl;
  const progressPct = exported ? 100 : Math.min(Math.max(pages, 0) * 10 + (pages > 0 ? 10 : 0), 95);
  const sanitizedThumb = snapshot.firstSlideSvg
    ? renderTrustedSlideSVG(snapshot.firstSlideSvg, snapshot.name || '')
    : '';
  const thumb = sanitizedThumb
    || '<svg viewBox="0 0 64 40" xmlns="http://www.w3.org/2000/svg"><rect width="64" height="40" fill="#ebe9fe"/><text x="32" y="24" font-size="9" fill="#6c5ce7" text-anchor="middle">PPT</text></svg>';
  const editorUrl = escapeHtml(getStandaloneEditorURL(rawName));
  const downloadBtn = exported
    ? `<a class="chat-ppt-open-btn secondary" href="${escapeHtml(withAuthURL(snapshot.exportUrl))}" target="_blank" rel="noopener" download>↓ 下载 PPTX</a>`
    : '';
  const editorBtn = exported
    ? `<a class="chat-ppt-open-btn" href="${editorUrl}" target="_blank" rel="noopener" data-action="open-editor" data-project-name="${escapeHtml(rawName)}">▶ 打开编辑器</a>`
    : '<button type="button" class="chat-ppt-open-btn disabled" disabled>生成完成后可编辑</button>';
  return `
    <div class="chat-ppt-card" data-project-name="${escapeHtml(rawName)}">
      <div class="chat-ppt-card-head">
        <div class="chat-ppt-card-icon">P</div>
        <div class="chat-ppt-card-name">${name}</div>
        <div class="chat-ppt-card-thumb" data-action="preview-slide">${thumb}</div>
      </div>
      <div class="chat-ppt-card-text">${exported ? '已生成完成的 PPT 项目，可在编辑器中查看或下载。' : `已生成 <strong>${pages}</strong> 页，正在持续更新…`}</div>
      <div class="chat-ppt-progress"><div class="chat-ppt-progress-bar" style="width:${progressPct}%"></div></div>
      <div class="chat-ppt-card-actions">
        ${editorBtn}
        ${downloadBtn}
      </div>
    </div>
  `;
}

// SVG 内可执行脚本的元素白名单外标签。foreignObject 能嵌入任意 HTML，script 显然要剔除。
const SVG_DANGEROUS_TAGS = new Set(['script', 'foreignobject']);

// sanitizeSVG 解析、清理后再序列化一段 SVG 字符串。
// 返回值要么是清理过的 SVG 字符串，要么是空串（解析失败 / 不是 svg 根）。
// 输入来自 LLM 生成的磁盘文件，必须按不可信源处理：
//   - 删除 <script> / <foreignObject>
//   - 去掉所有 on* 事件属性
//   - 去掉以 javascript: 开头的 href / xlink:href
function sanitizeSVG(rawSvg = '') {
  const trimmed = String(rawSvg).trim();
  if (!trimmed) return '';
  let doc;
  try {
    doc = new DOMParser().parseFromString(trimmed, 'image/svg+xml');
  } catch (error) {
    console.warn('sanitizeSVG parse failed', error);
    return '';
  }
  if (doc.querySelector('parsererror')) return '';
  const root = doc.documentElement;
  if (!root || root.tagName.toLowerCase() !== 'svg') return '';
  sanitizeSVGNode(root);
  return new XMLSerializer().serializeToString(root);
}

function sanitizeSVGNode(node) {
  if (!node) return;
  for (let i = node.children.length - 1; i >= 0; i -= 1) {
    const child = node.children[i];
    const tag = (child.tagName || '').toLowerCase();
    if (SVG_DANGEROUS_TAGS.has(tag)) {
      child.remove();
      continue;
    }
    for (const attr of Array.from(child.attributes)) {
      const name = attr.name.toLowerCase();
      if (name.startsWith('on')) {
        child.removeAttribute(attr.name);
        continue;
      }
      if ((name === 'href' || name === 'xlink:href') && /^\s*javascript:/i.test(attr.value)) {
        child.removeAttribute(attr.name);
      }
    }
    sanitizeSVGNode(child);
  }
}

// renderTrustedSlideSVG 是所有要 innerHTML 进 DOM 的页面 SVG 的统一入口。
// 先净化，再做 asset URL 重写。空字符串表示无可渲染内容（调用方应用占位）。
function renderTrustedSlideSVG(rawSvg = '', projectName = '') {
  const sanitized = sanitizeSVG(rawSvg);
  if (!sanitized) return '';
  return rewriteSvgAssetUrls(sanitized, projectName);
}

function rewriteSvgAssetUrls(svgContent = '', projectName = '') {
  if (!svgContent || !projectName) return svgContent || '';
  const assetBase = `${API_BASE}/projects/${encodeURIComponent(projectName)}/assets`;
  return String(svgContent).replace(
    /\b(xlink:href|href)=["']([^"']+)["']/gi,
    (full, attr, rawValue) => {
      const value = String(rawValue || '').trim();
      if (!value || value.startsWith('#') || value.startsWith('/') || /^data:/i.test(value) || /^https?:\/\//i.test(value)) {
        return full;
      }
      const normalized = value
        .replace(/^\.\/+/, '')
        .split('/')
        .map((segment) => encodeURIComponent(segment))
        .join('/');
      return `${attr}="${assetBase}/${normalized}"`;
    },
  );
}

function createMessageElement(role, html) {
  const fragment = elements.messageTemplate.content.cloneNode(true);
  const row = fragment.querySelector('.message-row');
  const bubble = fragment.querySelector('.message-bubble');

  row.classList.add(role);

  if (role === 'assistant') {
    const stack = document.createElement('div');
    stack.className = 'message-stack';
    bubble.innerHTML = html;
    stack.appendChild(bubble);
    row.appendChild(stack);
  } else {
    bubble.innerHTML = html;
  }

  elements.chatContainer.appendChild(fragment);
  const appendedRow = elements.chatContainer.lastElementChild;
  return {
    row: appendedRow,
    bubble: appendedRow.querySelector('.message-bubble'),
  };
}

function appendMessage(role, rawText = '', options = {}) {
  const record = {
    id: options.id || crypto.randomUUID(),
    role,
    content: rawText,
    uploads: normalizeUploads(options.uploads || []),
    tools: options.tools || [],
    pptSnapshot: options.pptSnapshot || null,
    row: null,
    bubble: null,
  };
  state.messages.push(record);
  hideHeroIfNeeded();
  const html = role === 'assistant'
    ? renderAssistantMessage(record)
    : renderUserMessage(rawText, record.uploads);
  const messageEls = createMessageElement(role, html);
  record.row = messageEls.row;
  record.bubble = messageEls.bubble;
  enhanceRenderedMessage(record.bubble);
  scrollChatToBottom();
  return record;
}

function updateAssistantMessage(record, patch = {}) {
  if (typeof patch.content === 'string') record.content = patch.content;
  if (patch.tool) {
    record.tools = record.tools || [];
    const existing = record.tools.find((item) => item.id === patch.tool.id);
    if (existing) Object.assign(existing, patch.tool);
    else record.tools.push(patch.tool);
  }
  if (patch.pptSnapshot !== undefined) record.pptSnapshot = patch.pptSnapshot;
  record.bubble.innerHTML = renderAssistantMessage(record);
  enhanceRenderedMessage(record.bubble);
  scrollChatToBottom();
  persistPendingStreamState();
}

/* ============================ uploads UI ============================ */

function toggleSendBtn() {
  const btn = elements.sendBtn;
  if (!btn) return;
  const hasInput = (elements.messageInput?.value.trim().length || 0) > 0 || state.uploads.length > 0;
  // 流式中：隐藏发送按钮（由 stopBtn 顶替），并 disable 防止键盘 Enter 透传。
  // 空闲：显示发送按钮，按是否有输入决定是否禁用。统一用 hidden / disabled，
  // 不再用 inline opacity / pointerEvents，避免状态残留。
  btn.classList.toggle('hidden', state.isStreaming);
  btn.disabled = state.isStreaming || !hasInput;
}

function renderUploads() {
  elements.uploadPreview.innerHTML = '';
  elements.uploadPreview.classList.toggle('hidden', state.uploads.length === 0);
  state.uploads.forEach((item) => {
    const card = document.createElement('div');
    card.className = `preview-item ${item.kind === 'image' && item.url ? 'image-file' : 'file-card'}`;
    card.dataset.kind = item.kind;
    card.dataset.id = item.id;
    const statusText = item.parseStatus === 'success'
      ? (item.kind === 'image' ? '图片预览可用' : '内容已提取')
      : item.parseStatus === 'failed'
        ? '解析失败，发送元信息'
        : '将发送文件元信息';
    card.innerHTML = item.kind === 'image' && item.url
      ? `
        <img src="${item.url}" alt="${escapeHtml(item.name)}" />
        <button class="preview-remove" type="button" data-id="${item.id}">×</button>
      `
      : `
        <button class="preview-remove" type="button" data-id="${item.id}">×</button>
        <div class="file-card-icon">${escapeHtml(getFileBadgeLabel(item))}</div>
        <div class="file-card-name" title="${escapeHtml(item.name)}">${escapeHtml(item.name)}</div>
        <div class="file-card-meta">${escapeHtml(formatFileSize(item.size))}</div>
        <div class="file-card-status">${escapeHtml(statusText)}</div>
      `;
    elements.uploadPreview.appendChild(card);
  });
  toggleSendBtn();
}

/* ============================ 会话列表 ============================ */

function renderSessionList() {
  elements.sessionList.innerHTML = '';
  const keyword = state.sessionSearchQuery.trim().toLowerCase();
  const visibleSessions = !keyword
    ? state.sessions
    : state.sessions.filter((session) => (session.title || '').toLowerCase().includes(keyword));

  if (visibleSessions.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'session-search-empty';
    empty.textContent = keyword ? '没有找到匹配的会话' : '暂无会话';
    elements.sessionList.appendChild(empty);
    return;
  }

  visibleSessions.forEach((session) => {
    const item = document.createElement('div');
    item.className = `session-item${state.openSessionMenuId === session.conversation_id ? ' menu-open' : ''}`;
    item.innerHTML = `
      <div class="session-chip${session.conversation_id === state.activeSessionId ? ' active' : ''}" data-session-id="${session.conversation_id}" role="button" tabindex="0">
        <span class="session-chip-label">${escapeHtml(session.title || '新会话')}</span>
        <button type="button" class="session-more-btn" data-action="toggle-menu" data-session-id="${session.conversation_id}" aria-label="更多操作">⋯</button>
      </div>
      <div class="session-actions">
        <button type="button" class="session-action-btn" data-action="rename" data-session-id="${session.conversation_id}">重命名</button>
        <button type="button" class="session-action-btn danger" data-action="delete" data-session-id="${session.conversation_id}">删除</button>
      </div>
    `;
    elements.sessionList.appendChild(item);
  });
}

/* ============================ API ============================ */

async function apiFetch(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (state.authToken && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${state.authToken}`);
  }
  const response = await fetch(`${API_BASE}${path}`, { ...options, headers });
  if (!response.ok) {
    const raw = await response.text().catch(() => '');
    // 后端返回的可能是 {code, msg} JSON、纯文本，也可能是 nginx 默认错误页。
    // 只把结构化的 msg 透传给用户，其它一律收敛成通用提示，避免把 stack/路径 toast 出来。
    let userMsg = `请求失败 (${response.status})`;
    try {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed.msg === 'string' && parsed.msg.trim()) {
        userMsg = parsed.msg.trim();
      }
    } catch {
      // raw 不是 JSON，保留通用提示
    }
    if (raw) console.warn(`apiFetch ${path} ${response.status}:`, raw);
    if (response.status === 401) {
      clearAuthSession(false);
      showAuthOverlay();
    }
    throw new Error(userMsg);
  }
  return response;
}

async function apiJSON(path, options = {}) {
  const response = await apiFetch(path, options);
  const result = await response.json();
  if (result.code !== 0) {
    throw new Error(result.msg || '请求失败');
  }
  return result.data;
}

function setAuthSession(auth) {
  const user = auth?.user || {};
  state.authToken = auth?.token || '';
  state.userId = user.user_id || '';
  state.username = user.username || '';
  if (state.authToken) localStorage.setItem(STORAGE_KEYS.authToken, state.authToken);
  if (state.userId) localStorage.setItem(STORAGE_KEYS.userId, state.userId);
  if (state.username) localStorage.setItem(STORAGE_KEYS.authUsername, state.username);
  updateAuthUI();
}

function clearAuthSession(callServer = true) {
  const token = state.authToken;
  state.authToken = '';
  state.userId = '';
  state.username = '';
  state.sessions = [];
  state.activeSessionId = '';
  state.messages = [];
  state.parentMessageId = '';
  localStorage.removeItem(STORAGE_KEYS.authToken);
  localStorage.removeItem(STORAGE_KEYS.userId);
  localStorage.removeItem(STORAGE_KEYS.authUsername);
  localStorage.removeItem(STORAGE_KEYS.activeSessionId);
  clearPendingStreamState();
  if (elements.chatContainer) elements.chatContainer.innerHTML = '';
  renderSessionList();
  updateChatHeader();
  updateAuthUI();
  if (callServer && token) {
    fetch(`${API_BASE}/auth/logout`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    }).catch(() => {});
  }
}

function updateAuthUI() {
  if (elements.chatUserName) {
    elements.chatUserName.textContent = state.username || '未登录';
  }
  elements.logoutBtn?.classList.toggle('hidden', !state.authToken);
}

function showAuthOverlay(message = '') {
  renderAuthMode();
  if (elements.authError) {
    elements.authError.textContent = message;
    elements.authError.classList.toggle('hidden', !message);
  }
  elements.authOverlay?.classList.remove('hidden');
  setTimeout(() => elements.authUsername?.focus(), 0);
}

function hideAuthOverlay() {
  elements.authOverlay?.classList.add('hidden');
  if (elements.authError) {
    elements.authError.textContent = '';
    elements.authError.classList.add('hidden');
  }
}

function renderAuthMode() {
  const isRegister = state.authMode === 'register';
  if (elements.authTitle) elements.authTitle.textContent = isRegister ? '创建账号' : '登录账号';
  if (elements.authSubmitBtn) elements.authSubmitBtn.textContent = isRegister ? '创建账号' : '登录';
  if (elements.authModeBtn) elements.authModeBtn.textContent = isRegister ? '已有账号，去登录' : '没有账号，创建一个';
  if (elements.authPassword) elements.authPassword.autocomplete = isRegister ? 'new-password' : 'current-password';
}

async function loadCurrentUser() {
  const user = await apiJSON('/auth/me');
  state.userId = user.user_id || '';
  state.username = user.username || '';
  if (state.userId) localStorage.setItem(STORAGE_KEYS.userId, state.userId);
  if (state.username) localStorage.setItem(STORAGE_KEYS.authUsername, state.username);
  updateAuthUI();
  return user;
}

async function handleAuthSubmit(event) {
  event.preventDefault();
  const username = elements.authUsername?.value.trim() || '';
  const password = elements.authPassword?.value || '';
  if (!username || !password) {
    showAuthOverlay('请输入账号和密码');
    return;
  }
  elements.authSubmitBtn.disabled = true;
  try {
    const endpoint = state.authMode === 'register' ? '/auth/register' : '/auth/login';
    const auth = await apiJSON(endpoint, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    setAuthSession(auth);
    hideAuthOverlay();
    await loadAppData();
  } catch (error) {
    showAuthOverlay(error.message || '登录失败');
  } finally {
    elements.authSubmitBtn.disabled = false;
  }
}

function isPPTIntent(text = '') {
  const normalized = String(text).toLowerCase();
  return ['ppt', 'powerpoint', 'slides', 'slide deck', '演示文稿', '幻灯片', '答辩', '汇报'].some((keyword) => normalized.includes(keyword));
}

function stripTemplateChoicePrompt(text = '') {
  return String(text).replace(/\s+/g, ' ').trim();
}

function escapeRegExp(text = '') {
  return String(text).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function templateDisplayName(template = {}) {
  return template?.label || template?.name || '';
}

function clearTemplateRecommendation() {
  state.templateRecommendation = null;
  state.awaitingTemplateDecision = false;
}

function applyRecommendedTemplateFromText(text = '') {
  if (!text || !state.pendingTemplateRequest || !Array.isArray(state.pptTemplates) || state.pptTemplates.length === 0) {
    return false;
  }

  const content = String(text);
  const candidates = state.pptTemplates.map((tpl) => {
    const aliases = [tpl.name, tpl.label].filter(Boolean);
    let score = 0;
    aliases.forEach((alias) => {
      if (!alias) return;
      const escaped = escapeRegExp(alias);
      if (new RegExp(`模板[「“\"]?${escaped}[」”\"]?`).test(content)) score += 4;
      if (new RegExp(`[「“\"]${escaped}[」”\"]`).test(content)) score += 3;
      if (content.includes(alias)) score += 2;
    });
    return { tpl, score };
  }).filter((item) => item.score > 0).sort((a, b) => b.score - a.score);

  if (!candidates.length) return false;

  const recommended = candidates[0].tpl;
  const reason = content
    .replace(/\*\*/g, '')
    .replace(/推荐模板：.*$/m, '')
    .replace(/我已为你打开模板选择页[\s\S]*$/m, '')
    .replace(/推荐理由[:：]?/g, '')
    .replace(/\s+/g, ' ')
    .trim();
  state.selectedTemplateName = recommended.name;
  state.templateRecommendation = {
    templateName: recommended.name,
    title: `推荐使用「${templateDisplayName(recommended)}」`,
    reason: reason || '你可以直接使用推荐模板，也可以改选其他模板。',
  };
  state.awaitingTemplateDecision = true;
  renderTemplateLibrary();
  setActiveView('templates');
  return true;
}

function buildTemplateRecommendationQuery(text = '') {
  return `${stripTemplateChoicePrompt(text)}\n\n这是一个 PPT 需求。请先调用 list_ppt_templates 工具查看可用模板，从中推荐 1 个最合适的模板。回复格式必须包含：\n1. 明确写出“推荐模板：<template_name>”\n2. 用 2-4 句说明推荐理由\n3. 结尾说明“我已为你打开模板选择页，你可以直接使用这个模板，也可以改选其他模板，或回复不使用模板”。\n此轮不要开始生成 PPT，不要创建项目，不要复制模板，不要继续执行生成步骤。`;
}

async function loadPPTTemplates() {
  const data = await apiJSON('/ppt/templates');
  state.pptTemplates = Array.isArray(data) ? data : [];
  if (!state.selectedTemplateName && state.pptTemplates[0]) {
    state.selectedTemplateName = state.pptTemplates[0].name;
  }
  renderTemplateLibrary();
}

function getSelectedTemplate() {
  return state.pptTemplates.find((item) => item.name === state.selectedTemplateName) || state.pptTemplates[0] || null;
}

function getTemplatePreviewURLs(template) {
  if (!template) return [];
  const previews = Array.isArray(template.preview_svg_urls)
    ? template.preview_svg_urls.filter((item) => typeof item === 'string' && item.trim())
    : [];
  if (previews.length > 0) return previews;
  return template.preview_svg_url ? [template.preview_svg_url] : [];
}

function renderTemplateLibrary() {
  const selected = getSelectedTemplate();
  const selectedPreviewURLs = getTemplatePreviewURLs(selected);
  const primaryPreview = selectedPreviewURLs[0] || '';
  const secondaryPreviews = selectedPreviewURLs.slice(1, 5);
  const recommendedTemplateName = state.templateRecommendation?.templateName || '';

  if (elements.tplGrid) {
    elements.tplGrid.innerHTML = state.pptTemplates.map((tpl) => `
      <div class="tpl-card${tpl.name === recommendedTemplateName ? ' is-recommended' : ''}" data-template-name="${escapeHtml(tpl.name)}">
        <div class="tpl-card-thumb${tpl.name === selected?.name ? ' selected' : ''}" data-template-name="${escapeHtml(tpl.name)}">
          ${getTemplatePreviewURLs(tpl)[0] ? `<img class="tpl-card-image" src="${API_ORIGIN}${getTemplatePreviewURLs(tpl)[0]}" alt="${escapeHtml(tpl.label || tpl.name)}" />` : '<div class="tpl-card-fallback">PPT</div>'}
        </div>
        <div class="tpl-card-label-row">
          <div class="tpl-card-label">${escapeHtml(tpl.label || tpl.name)}</div>
          ${tpl.name === recommendedTemplateName ? '<span class="tpl-card-badge">AI 推荐</span>' : ''}
        </div>
        <div class="tpl-card-desc">${escapeHtml(tpl.summary || '')}</div>
      </div>
    `).join('');
  }

  if (elements.tplRecommendationBanner) {
    const hasRecommendation = !!state.templateRecommendation;
    elements.tplRecommendationBanner.classList.toggle('hidden', !hasRecommendation);
    if (hasRecommendation) {
      elements.tplRecommendationTitle.textContent = state.templateRecommendation.title || 'AI 推荐模板';
      elements.tplRecommendationReason.textContent = state.templateRecommendation.reason || '你可以直接使用推荐模板，也可以改选其他模板。';
    }
  }

  if (elements.tplPreviewLarge) {
    elements.tplPreviewLarge.innerHTML = primaryPreview
      ? `<img class="tpl-preview-image" src="${API_ORIGIN}${primaryPreview}" alt="${escapeHtml(selected.label || selected.name)}" />`
      : '<div class="tpl-preview-empty">暂无预览</div>';
  }

  if (elements.tplPreviewGrid) {
    elements.tplPreviewGrid.innerHTML = secondaryPreviews.map((url) => `
      <div class="tpl-grid-thumb">
        <img class="tpl-grid-image" src="${API_ORIGIN}${url}" alt="模板预览" />
      </div>
    `).join('') || '<div class="tpl-preview-empty">暂无缩略图</div>';
  }

  if (elements.tplGenerateBtnLabel) {
    elements.tplGenerateBtnLabel.textContent = state.awaitingTemplateDecision ? '使用当前模板生成' : '开始生成PPT';
  }
}

async function ensureConversation() {
  if (state.activeSessionId) return state.activeSessionId;
  const created = await apiJSON('/conversation', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user_id: state.userId, title: '新会话' }),
  });
  state.activeSessionId = created.conversation_id;
  await loadConversations();
  return state.activeSessionId;
}

async function loadConversations() {
  const data = await apiJSON('/conversation');
  state.sessions = Array.isArray(data) ? data : [];
  if (state.activeSessionId && !state.sessions.some((item) => item.conversation_id === state.activeSessionId)) {
    state.activeSessionId = null;
  }
  if (!state.activeSessionId && state.sessions[0]) {
    state.activeSessionId = state.sessions[0].conversation_id;
  }
  persistActiveSessionId();
  renderSessionList();
  updateChatHeader();
}

function updateChatHeader() {
  const session = state.sessions.find((item) => item.conversation_id === state.activeSessionId);
  elements.chatHeaderTitle.textContent = session?.title || '新对话';
}

// resetPPTContext 在切换会话或加载历史时清空 PPT 相关状态。
// （历史里的 exportUrl 由 restoreProjectFromURL 通过后端 /projects/:name/pptist 拉回，
// 不再在前端做正则提取。）
function resetPPTContext() {
  state.pptProject = null;
  state.activeSlideIndex = 1;
  state.hasAutoSwitchedToEditor = false;
  clearTemplateRecommendation();
  renderRightRails();
}

function extractProjectNameFromHistory(messages = []) {
  for (let i = messages.length - 1; i >= 0; i -= 1) {
    const message = messages[i];
    const candidates = [message.response];
    (message.rounds || []).forEach((round) => {
      if (round.role === 'tool' && round.content) candidates.push(round.content);
    });

    for (const candidate of candidates) {
      const text = String(candidate || '');
      if (!text) continue;

      const exportMatch = text.match(/\/api\/projects\/([^/\s]+)\/exports\//);
      if (exportMatch) return exportMatch[1];

      const projectNameMatch = text.match(/\b([A-Za-z0-9_-]+_ppt\d+_\d{8})\b/);
      if (projectNameMatch) return projectNameMatch[1];

      const createdMatch = text.match(/project created at\s+([^\n]+)/i);
      if (createdMatch) {
        const rawPath = createdMatch[1].trim().replace(/[.)\]]+$/, '');
        const parts = rawPath.split(/[\\/]/).filter(Boolean);
        if (parts.length > 0) return parts[parts.length - 1];
      }
    }
  }
  return '';
}

function buildPPTSnapshotFromProject(project) {
  if (!project) return null;
  const pages = (project.pages || []).filter(Boolean);
  return {
    name: project.name || 'PPT 项目',
    pages: pages.length,
    exportUrl: project.exportUrl
      ? withAuthURL(/^https?:\/\//i.test(project.exportUrl) ? project.exportUrl : `${API_ORIGIN}${project.exportUrl}`)
      : '',
    firstSlideSvg: pages[0]?.svgContent || '',
  };
}

function normalizeRestoredPPTPages(project = {}, projectName = '') {
  const slides = Array.isArray(project.slides) ? project.slides : [];
  if (slides.length > 0) {
    return slides.map((slide, index) => {
      const imageSrc = slide?.background?.image?.src || '';
      return {
        pageIndex: index + 1,
        fileName: imageSrc ? imageSrc.split('/').pop() : `slide_${index + 1}.svg`,
        svgContent: imageSrc ? buildRestoredSlidePreviewSVG(imageSrc, projectName) : '',
      };
    });
  }

  const pageCount = Number(project.page_count) || 0;
  return Array.from({ length: Math.max(pageCount, 0) }, (_, index) => ({
    pageIndex: index + 1,
    fileName: '',
    svgContent: '',
  }));
}

function buildRestoredSlidePreviewSVG(imageSrc = '', projectName = '') {
  const absoluteSrc = /^https?:\/\//i.test(imageSrc)
    ? imageSrc
    : `${API_ORIGIN}${imageSrc.startsWith('/') ? imageSrc : `/${imageSrc}`}`;
  const authedSrc = withAuthURL(absoluteSrc);
  return `
    <svg viewBox="0 0 1000 562.5" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="${escapeHtml(projectName || 'PPT slide')}">
      <rect width="1000" height="562.5" fill="#f8fafc"/>
      <image href="${escapeHtml(authedSrc)}" x="0" y="0" width="1000" height="562.5" preserveAspectRatio="xMidYMid meet"/>
    </svg>
  `.trim();
}

function upsertProjectCardMessage(project) {
  const snapshot = buildPPTSnapshotFromProject(project);
  if (!snapshot) return;

  const existing = state.messages.find((message) => (
    message.role === 'assistant'
    && message.pptSnapshot
    && message.pptSnapshot.name === snapshot.name
  ));

  if (existing) {
    updateAssistantMessage(existing, { pptSnapshot: snapshot });
    return;
  }

  appendMessage('assistant', '', {
    id: `restored-ppt-${snapshot.name}`,
    pptSnapshot: snapshot,
  });
}

async function restoreProjectFromURL(projectName = getProjectNameFromURL()) {
  if (!projectName) return;

  const project = await apiJSON(`/projects/${encodeURIComponent(projectName)}/pptist`);
  const restoredName = project.project_name || projectName;

  state.pptProject = {
    name: restoredName,
    path: '',
    exportUrl: project.export_url
      ? withAuthURL(/^https?:\/\//i.test(project.export_url) ? project.export_url : `${API_ORIGIN}${project.export_url}`)
      : '',
    fileName: project.file_name || '',
    pages: normalizeRestoredPPTPages(project, restoredName),
  };

  renderRightRails();
  upsertProjectCardMessage(state.pptProject);
  syncPPTSnapshotIntoLastAssistant();
}

async function loadConversationMessages(conversationId) {
  const data = await apiJSON(`/conversation/${conversationId}/message`);
  state.messages = [];
  state.parentMessageId = '';
  elements.chatContainer.innerHTML = '';

  data.forEach((message) => {
    appendMessage('user', message.query);
    const tools = [];
    (message.rounds || []).forEach((round) => {
      if (round.role === 'assistant' && Array.isArray(round.tool_calls)) {
        round.tool_calls.forEach((toolCall) => {
          tools.push({
            id: toolCall.id,
            toolName: toolCall.name,
            arguments: toolCall.arguments,
            result: '',
          });
        });
      }
      if (round.role === 'tool') {
        const target = tools.find((item) => item.id === round.tool_id);
        if (target) {
          target.result = round.content || '';
          target.toolName = round.tool_name || target.toolName;
        }
      }
    });
    appendMessage('assistant', message.response || '', {
      id: message.message_id,
      tools,
    });
    state.parentMessageId = message.message_id;
  });

  resetPPTContext();
  resetPipelineState();
  const historyProjectName = extractProjectNameFromHistory(data);
  if (historyProjectName) {
    await restoreProjectFromURL(historyProjectName);
  }
  restorePendingStreamState(data);
  hideHeroIfNeeded();
  updateChatHeader();
}

async function switchSession(sessionId) {
  if (state.isStreaming || sessionId === state.activeSessionId) return;
  state.activeSessionId = sessionId;
  persistActiveSessionId();
  state.openSessionMenuId = null;
  renderSessionList();
  await loadConversationMessages(sessionId);
}

function resetPipelineState() {
  state.pipelineStage = '';
  state.pipelinePageOptions = [];
  state.pipelineOutline = null;
  state.pipelineAwaitingPagePick = false;
  state.pipelineAwaitingOutlineConfirm = false;
  if (elements.pipelineBar) {
    elements.pipelineBar.innerHTML = '';
    elements.pipelineBar.classList.add('hidden');
  }
  if (elements.pipelineCards) elements.pipelineCards.innerHTML = '';
}

async function createNewSession() {
  if (state.isStreaming) return;
  state.messages = [];
  state.parentMessageId = '';
  elements.chatContainer.innerHTML = '';
  state.pptProject = null;
  state.activeSlideIndex = 1;
  state.hasAutoSwitchedToEditor = false;
  clearTemplateRecommendation();
  state.pendingTemplateRequest = null;
  clearPendingStreamState();
  resetPipelineState();
  renderRightRails();
  renderTemplateLibrary();
  hideHeroIfNeeded();
  const created = await apiJSON('/conversation', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ user_id: state.userId, title: '新会话' }),
  });
  state.activeSessionId = created.conversation_id;
  persistActiveSessionId();
  await loadConversations();
  setActiveView('chat');
}

async function renameSession(sessionId) {
  const session = state.sessions.find((item) => item.conversation_id === sessionId);
  const currentTitle = session?.title || '新会话';
  const nextTitle = window.prompt('请输入新的会话名称：', currentTitle);
  if (nextTitle === null) return;
  const trimmed = nextTitle.trim();
  if (!trimmed) return;
  await apiJSON(`/conversation/${sessionId}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ title: trimmed }),
  });
  await loadConversations();
}

async function deleteSession(sessionId) {
  await apiJSON(`/conversation/${sessionId}`, { method: 'DELETE' });
  if (state.activeSessionId === sessionId) {
    state.activeSessionId = '';
    persistActiveSessionId();
    state.parentMessageId = '';
    state.messages = [];
    elements.chatContainer.innerHTML = '';
    state.pptProject = null;
    clearTemplateRecommendation();
    state.pendingTemplateRequest = null;
    clearPendingStreamState();
    renderRightRails();
    renderTemplateLibrary();
    hideHeroIfNeeded();
  }
  await loadConversations();
  if (!state.activeSessionId && state.sessions[0]) {
    await switchSession(state.sessions[0].conversation_id);
  }
}

/* ============================ 共享右栏渲染 ============================ */

function getRightRailSnapshot() {
  const ppt = state.pptProject;
  if (!ppt) {
    return {
      title: '尚未生成项目',
      hasProject: false,
      aiText: '在左侧对话中描述你的 PPT 需求，AI 会实时生成大纲与幻灯片，并显示在这里。',
      suggestions: [
        '帮我做一份关于团队季度复盘的 PPT',
        '生成一份新员工入职培训 PPT',
        '关于人工智能发展趋势的演讲稿',
      ],
      pages: 0,
      firstSlideSvg: '',
    };
  }
  const pages = (ppt.pages || []).filter(Boolean);
  const firstSlide = pages[0];
  return {
    title: ppt.name || 'PPT 项目',
    hasProject: true,
    aiText: ppt.exportUrl
      ? `已为你生成关于 <strong>${escapeHtml(ppt.name || '')}</strong> 的完整 PPT，共 ${pages.length} 页，可在编辑器中查看或下载。`
      : `正在生成关于 <strong>${escapeHtml(ppt.name || '')}</strong> 的 PPT，已就绪 <strong>${pages.length}</strong> 页…`,
    suggestions: CONFIG.defaultSuggestions,
    pages: pages.length,
    firstSlideSvg: firstSlide?.svgContent || '',
  };
}

function renderRightRails() {
  const snapshot = getRightRailSnapshot();
  elements.rightRails.forEach((rail) => {
    if (!rail.isConnected || rail.dataset.rail === 'editor') {
      return;
    }
    if (!rail.dataset.bound) {
      rail.dataset.bound = '1';
      rail.innerHTML = '';
      rail.appendChild(elements.rightRailTemplate.content.cloneNode(true));
    }
    const titleEl = rail.querySelector('.rp-title');
    const aiEl = rail.querySelector('.rp-ai-text');
    const cardEl = rail.querySelector('.rp-ppt-card');
    const cardName = rail.querySelector('.rp-ppt-name');
    const cardThumb = rail.querySelector('.rp-ppt-thumb');
    const suggestList = rail.querySelector('.rp-suggest-list');

    titleEl.textContent = snapshot.title;
    aiEl.innerHTML = snapshot.aiText;

    if (snapshot.hasProject) {
      cardEl.classList.remove('hidden');
      cardName.textContent = snapshot.title;
      cardThumb.innerHTML = renderTrustedSlideSVG(snapshot.firstSlideSvg, snapshot.title)
        || '<svg viewBox="0 0 64 40" xmlns="http://www.w3.org/2000/svg"><rect width="64" height="40" fill="#ebe9fe"/><text x="32" y="25" font-size="9" fill="#6c5ce7" text-anchor="middle">PPT</text></svg>';
    } else {
      cardEl.classList.add('hidden');
    }

    suggestList.innerHTML = snapshot.suggestions.map((text) => `
      <div class="rp-suggest" data-prompt="${escapeHtml(text)}">
        <span>${escapeHtml(text)}</span>
        <span class="arr">›</span>
      </div>
    `).join('');
  });
}

/* ============================ Pipeline v2: 进度条 + intake/outline 卡 ============================ */

const PIPELINE_STAGE_ORDER = ['intake', 'research', 'outline', 'layout', 'render', 'export'];
const PIPELINE_STAGE_LABEL = {
  intake: '需求确认',
  research: '联网调研',
  outline: '大纲架构',
  layout: '版式规划',
  render: '页面生成',
  export: '导出 PPTX',
  legacy: '兼容模式',
};

function renderPipelineProgress() {
  const bar = elements.pipelineBar;
  if (!bar) return;
  const stage = state.pipelineStage;
  if (!stage) {
    bar.innerHTML = '';
    bar.classList.add('hidden');
    return;
  }
  bar.classList.remove('hidden');
  if (stage === 'legacy') {
    bar.innerHTML = '<span class="pipeline-step-legacy">⚡ 兼容模式</span>';
    return;
  }
  const idx = PIPELINE_STAGE_ORDER.indexOf(stage);
  bar.innerHTML = PIPELINE_STAGE_ORDER.map((s, i) => {
    const cls = i < idx ? 'done' : i === idx ? 'active' : 'pending';
    const icon = i < idx ? '✓' : String(i + 1);
    return `<div class="pipeline-step ${cls}" title="${escapeHtml(PIPELINE_STAGE_LABEL[s] || s)}">
      <div class="pipeline-step-dot">${icon}</div>
      <div class="pipeline-step-label">${escapeHtml(PIPELINE_STAGE_LABEL[s] || s)}</div>
    </div>`;
  }).join('');
  scrollChatToBottom();
}

function updateThinkingBlock(record, text, isDone = false) {
  const stack = record.row?.querySelector('.message-stack');
  if (!stack) return;
  if (!record.thinkingEl) {
    const details = document.createElement('details');
    details.className = 'thinking-block';
    details.setAttribute('open', '');
    details.innerHTML = `
      <summary>
        <div class="thinking-block-spinner"></div>
        <span class="thinking-block-label">深度思考中…</span>
      </summary>
      <div class="thinking-block-body"></div>
    `;
    stack.insertBefore(details, record.bubble);
    record.thinkingEl = details;
  }
  const body = record.thinkingEl.querySelector('.thinking-block-body');
  if (body) body.textContent = text;
  if (isDone) {
    record.thinkingEl.classList.add('done');
    record.thinkingEl.removeAttribute('open');
    const label = record.thinkingEl.querySelector('.thinking-block-label');
    if (label) label.textContent = '已完成深度思考';
  }
}

// 从 assistant 文本里抽取 [INTAKE] / [PPT_OUTLINE] 标签内容，渲染交互卡。
// 调用频率较高（每个 content delta 都会触发），所以做幂等：解析失败或重复内容直接跳过。
function tryHarvestPipelineTags(text) {
  const intake = matchTag(text, 'INTAKE');
  if (intake && !state.pipelineAwaitingPagePick) {
    try {
      const parsed = JSON.parse(intake);
      if (Array.isArray(parsed.page_options) && parsed.page_options.length > 0) {
        state.pipelinePageOptions = parsed.page_options;
        state.pipelineAwaitingPagePick = true;
        renderIntakeCard(parsed);
      }
    } catch (e) { /* 标签未闭合或 JSON 还在流式拼接，忽略 */ }
  }
  const outlineRaw = matchTag(text, 'PPT_OUTLINE');
  if (outlineRaw && !state.pipelineAwaitingOutlineConfirm) {
    try {
      const parsed = JSON.parse(outlineRaw);
      if (parsed.ppt_outline) {
        state.pipelineOutline = parsed.ppt_outline;
        state.pipelineAwaitingOutlineConfirm = true;
        renderOutlineCard(parsed.ppt_outline);
      }
    } catch (e) { /* 同上 */ }
  }
}

function matchTag(text, tagName) {
  const re = new RegExp(`\\[${tagName}\\]([\\s\\S]*?)\\[/${tagName}\\]`);
  const m = text.match(re);
  return m ? m[1].trim() : '';
}

function renderIntakeCard(intake) {
  const host = elements.pipelineCards;
  if (!host) return;
  const opts = (intake.page_options || []).map((opt) => `
    <button class="pipeline-pick" data-key="${escapeHtml(opt.key)}" data-range="${escapeHtml(opt.range)}">
      <div class="pipeline-pick-head">
        <span class="pipeline-pick-label">${escapeHtml(opt.label)}</span>
        ${opt.recommend ? '<span class="pipeline-pick-star">★ 推荐</span>' : ''}
      </div>
      <div class="pipeline-pick-range">${escapeHtml(opt.range)}</div>
      <div class="pipeline-pick-suit">${escapeHtml(opt.suitable || '')}</div>
    </button>
  `).join('');
  const card = document.createElement('div');
  card.className = 'pipeline-card pipeline-intake';
  card.innerHTML = `
    <div class="pipeline-card-title">请选择页数档位</div>
    <div class="pipeline-card-sub">主题：${escapeHtml(intake.topic || '')} · 受众：${escapeHtml(intake.audience || '')}</div>
    <div class="pipeline-pick-grid">${opts}</div>
  `;
  card.addEventListener('click', (ev) => {
    const btn = ev.target.closest('.pipeline-pick');
    if (!btn) return;
    const range = btn.dataset.range || '标准 15-20 页';
    submitPipelineUserPick(`我选 ${range}`);
    card.remove();
    state.pipelineAwaitingPagePick = false;
  });
  host.innerHTML = '';
  host.appendChild(card);
}

function renderOutlineCard(outline) {
  const host = elements.pipelineCards;
  if (!host) return;
  const sections = (outline.parts || []).map((part, i) => `
    <details class="pipeline-outline-part" ${i < 2 ? 'open' : ''}>
      <summary>${escapeHtml(part.part_title || '')}</summary>
      <ul>${(part.pages || []).map((p) => `<li>${escapeHtml(p.title || '')}</li>`).join('')}</ul>
    </details>
  `).join('');
  const card = document.createElement('div');
  card.className = 'pipeline-card pipeline-outline';
  card.innerHTML = `
    <div class="pipeline-card-title">大纲草案</div>
    <div class="pipeline-card-sub">${escapeHtml(outline.cover?.title || '')} — ${escapeHtml(outline.cover?.sub_title || '')}</div>
    <div class="pipeline-outline-toc">${(outline.table_of_contents?.content || []).map((t) => `<span>${escapeHtml(t)}</span>`).join('')}</div>
    ${sections}
    <div class="pipeline-card-actions">
      <button class="pipeline-confirm">确认大纲，继续</button>
      <button class="pipeline-reject">需要调整</button>
    </div>
  `;
  card.querySelector('.pipeline-confirm')?.addEventListener('click', () => {
    submitPipelineUserPick('确认大纲，继续下一阶段');
    card.remove();
    state.pipelineAwaitingOutlineConfirm = false;
  });
  card.querySelector('.pipeline-reject')?.addEventListener('click', () => {
    elements.messageInput?.focus();
    showToast('请在输入框写明调整意见后发送', 'info');
  });
  host.innerHTML = '';
  host.appendChild(card);
}

function submitPipelineUserPick(text) {
  if (!elements.messageInput) return;
  elements.messageInput.value = text;
  toggleSendBtn();
  elements.sendBtn?.click();
}

/* ============================ PPT 事件处理 ============================ */

function syncPPTSnapshotIntoLastAssistant() {
  // 找到最近一条 assistant 消息，把当前 PPT 快照贴上去（用于聊天内卡片）
  for (let i = state.messages.length - 1; i >= 0; i -= 1) {
    const m = state.messages[i];
    if (m.role === 'assistant') {
      const ppt = state.pptProject;
      const snapshot = ppt
        ? {
          name: ppt.name,
          pages: (ppt.pages || []).filter(Boolean).length,
          exportUrl: ppt.exportUrl
            ? withAuthURL(/^https?:\/\//i.test(ppt.exportUrl) ? ppt.exportUrl : `${API_ORIGIN}${ppt.exportUrl}`)
            : '',
          firstSlideSvg: (ppt.pages || []).filter(Boolean)[0]?.svgContent || '',
        }
        : null;
      updateAssistantMessage(m, { pptSnapshot: snapshot });
      return;
    }
  }
}

function handlePPTEvent(payload) {
  if (payload.event === 'ppt_project_created') {
    clearTemplateRecommendation();
    state.pendingTemplateRequest = null;
    state.pptProject = {
      name: payload.ppt_project_name,
      path: payload.ppt_project_path,
      pages: [],
      exportUrl: '',
      fileName: '',
    };
    state.activeSlideIndex = 1;
    state.hasAutoSwitchedToEditor = false;
    renderRightRails();
    syncPPTSnapshotIntoLastAssistant();
    persistPendingStreamState(true);
    showToast(`项目已创建：${payload.ppt_project_name}`, 'success');
    return;
  }

  if (!state.pptProject) return;

  if (payload.event === 'ppt_page_svg') {
    const page = {
      pageIndex: payload.ppt_page_index,
      fileName: payload.ppt_file_name,
      svgContent: payload.ppt_svg_content,
    };
    state.pptProject.pages[payload.ppt_page_index - 1] = page;
    state.pptProject.pages = state.pptProject.pages
      .filter(Boolean)
      .sort((a, b) => a.pageIndex - b.pageIndex);
    if (!state.activeSlideIndex || state.activeSlideIndex < 1) {
      state.activeSlideIndex = page.pageIndex;
    }
    renderRightRails();
    syncPPTSnapshotIntoLastAssistant();
    persistPendingStreamState(true);
    return;
  }

  if (payload.event === 'ppt_exported') {
    state.pptProject.exportUrl = payload.ppt_pptx_url
      ? withAuthURL(/^https?:\/\//i.test(payload.ppt_pptx_url) ? payload.ppt_pptx_url : `${API_ORIGIN}${payload.ppt_pptx_url}`)
      : '';
    state.pptProject.fileName = payload.ppt_file_name;
    renderRightRails();
    syncPPTSnapshotIntoLastAssistant();
    persistPendingStreamState(true);
    showToast('PPTX 导出完成', 'success');
  }
}

/* ============================ SSE 流式发送 ============================ */

async function streamChat(userText, uploads = []) {
  const conversationId = await ensureConversation();
  const assistantRecord = appendMessage('assistant', '');
  assistantRecord.bubble.classList.add('is-streaming');
  assistantRecord.thinkingEl = null;
  setStreamingStatus(true);
  persistPendingStreamState();
  state.abortController = new AbortController();
  const query = buildBackendQuery(userText, uploads);

  const response = await apiFetch(`/conversation/${conversationId}/message`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      user_id: state.userId,
      query,
      parent_message_id: state.parentMessageId,
    }),
    signal: state.abortController.signal,
  });

  const reader = response.body?.getReader();
  if (!reader) throw new Error('SSE 响应体为空');
  const decoder = new TextDecoder('utf-8');
  let buffer = '';
  let assistantContent = '';
  let thinkingText = '';
  const toolsByCallID = new Map();

  const handleSSEPayload = (payload) => {
    const toolCallKey = payload.tool_call_id || payload.tool_call || '';

    if (!state.activeMessageId && payload.message_id) {
      state.activeMessageId = payload.message_id;
      persistPendingStreamState();
    }

    if (payload.stage && payload.stage !== state.pipelineStage) {
      state.pipelineStage = payload.stage;
      renderPipelineProgress();
    }

    if (payload.event === 'reasoning') {
      thinkingText += payload.reasoning_content || '';
      updateThinkingBlock(assistantRecord, thinkingText, false);
      return;
    }

    if (payload.event === 'content' || payload.event === 'error') {
      if (thinkingText && assistantRecord.thinkingEl && !assistantRecord.thinkingEl.classList.contains('done')) {
        updateThinkingBlock(assistantRecord, thinkingText, true);
      }
      assistantContent += payload.content || '';
      updateAssistantMessage(assistantRecord, { content: assistantContent });
      if (state.pendingTemplateRequest && state.awaitingTemplateDecision && payload.event === 'content') {
        applyRecommendedTemplateFromText(assistantContent);
      }
      if (payload.stage === 'intake' || payload.stage === 'outline') {
        tryHarvestPipelineTags(assistantContent);
      }
    }

    if (payload.event === 'tool_call') {
      const tool = {
        id: toolCallKey || `${payload.tool_call}-${Date.now()}-${Math.random()}`,
        toolName: payload.tool_call,
        arguments: payload.tool_arguments || '',
        result: '',
      };
      toolsByCallID.set(tool.id, tool);
      updateAssistantMessage(assistantRecord, { tool });
    }

    if (payload.event === 'tool_result') {
      const tool = toolsByCallID.get(toolCallKey) || {
        id: toolCallKey || `${payload.tool_call}-${Date.now()}-${Math.random()}`,
        toolName: payload.tool_call,
        arguments: payload.tool_arguments || '',
        result: '',
      };
      tool.arguments = payload.tool_arguments || tool.arguments;
      tool.result = payload.tool_result || '';
      toolsByCallID.set(tool.id, tool);
      updateAssistantMessage(assistantRecord, { tool });
      persistPendingStreamState(true);
    }

    if (payload.event && payload.event.startsWith('ppt_')) {
      handlePPTEvent(payload);
    }
  };

  // 单条 SSE 事件解析。失败的 chunk 直接跳过，避免坏一条数据中断整个流。
  // 按 SSE 规范，一个事件可能包含多行 `data:`，需要按 `\n` 拼接后再 parse。
  const handleSSEChunk = (chunk) => {
    const dataParts = [];
    for (const line of chunk.split('\n')) {
      if (!line.startsWith('data:')) continue;
      // 规范说前导单空格可选，去掉以兼容 `data: x` 与 `data:x` 两种写法。
      dataParts.push(line.slice(5).replace(/^ /, ''));
    }
    if (dataParts.length === 0) return;
    const raw = dataParts.join('\n').trim();
    if (!raw) return;
    let payload;
    try {
      payload = JSON.parse(raw);
    } catch (error) {
      console.warn('SSE chunk parse failed', error, raw);
      return;
    }
    handleSSEPayload(payload);
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const chunks = buffer.split('\n\n');
    buffer = chunks.pop() || '';
    for (const chunk of chunks) handleSSEChunk(chunk);
  }

  // 流结束时把 decoder 内尚未输出的字节 flush 出来，再处理 buffer 残留，
  // 兜住末尾事件没有 \n\n 终止符的情况。
  buffer += decoder.decode();
  const trailing = buffer.trim();
  if (trailing) handleSSEChunk(trailing);

  // 收尾：移除流式光标，关闭思考块
  assistantRecord.bubble.classList.remove('is-streaming');
  if (thinkingText && assistantRecord.thinkingEl) {
    updateThinkingBlock(assistantRecord, thinkingText, true);
  }
}

function buildBackendQuery(userText, uploads = []) {
  const normalized = normalizeUploads(uploads);
  if (normalized.length === 0) return userText;
  const attachmentNotes = normalized.map((item, index) => {
    const lines = [`附件 ${index + 1}：${item.name}`];
    lines.push(`- 类型：${item.mimeType || item.kind}`);
    lines.push(`- 大小：${formatFileSize(item.size)}`);
    if (item.textContent) {
      lines.push('- 提取内容：');
      lines.push(item.textContent);
    } else {
      lines.push('- 说明：当前仅提供文件元信息。');
    }
    return lines.join('\n');
  }).join('\n\n');
  return `${userText || '请结合附件内容进行处理'}\n\n以下是本次上传附件的补充信息：\n${attachmentNotes}`;
}

async function handleSend(customText, providedUploads, options = {}) {
  const text = typeof customText === 'string' ? customText : elements.messageInput.value.trim();
  const displayText = typeof options.displayText === 'string' ? options.displayText : text;
  const backendText = typeof options.backendText === 'string' ? options.backendText : text;
  const skipTemplateRecommendation = !!options.skipTemplateRecommendation;
  const currentUploads = Array.isArray(providedUploads) ? providedUploads : [...state.uploads];
  if ((!text && currentUploads.length === 0) || state.isStreaming) return;

  const normalizedText = stripTemplateChoicePrompt(text);
  if (state.awaitingTemplateDecision && /^(不使用模板|不用模板|不要模板|自由设计|直接生成|不走模板)$/i.test(normalizedText)) {
    const pending = state.pendingTemplateRequest;
    if (!pending?.text) {
      showToast('当前没有待确认的 PPT 请求', 'info');
      return;
    }
    clearTemplateRecommendation();
    state.pendingTemplateRequest = null;
    state.uploads = [];
    renderUploads();
    await handleSend(
      pending.text,
      pending.uploads || [],
      {
        displayText: '不使用模板，直接开始生成',
        backendText: `${pending.text}\n\n不要使用任何预设模板，直接按需求自由设计并开始生成。`,
        skipTemplateRecommendation: true,
      },
    );
    return;
  }

  if (!skipTemplateRecommendation && isPPTIntent(text) && !state.pendingTemplateRequest) {
    state.pendingTemplateRequest = {
      text: normalizedText,
      uploads: [...currentUploads],
    };
    clearTemplateRecommendation();
    state.awaitingTemplateDecision = true;
    if (!state.pptTemplates.length) {
      try {
        await loadPPTTemplates();
      } catch (error) {
        showToast(`模板加载失败：${error.message}`, 'error');
        state.pendingTemplateRequest = null;
        state.awaitingTemplateDecision = false;
        return;
      }
    }
    return handleSend(
      text,
      currentUploads,
      {
        displayText: text,
        backendText: buildTemplateRecommendationQuery(text),
        skipTemplateRecommendation: true,
      },
    );
  }

  // 发送时确保切回 chat view
  setActiveView('chat');

  const uploads = [...currentUploads];
  appendMessage('user', displayText, { uploads });
  elements.messageInput.value = '';
  autoResizeTextarea();
  if (!Array.isArray(providedUploads)) {
    state.uploads = [];
    renderUploads();
  }

  try {
    await streamChat(backendText, uploads);
    await loadConversations();
  } catch (error) {
    if (error.name === 'AbortError') {
      showToast('已停止生成', 'info');
    } else {
      showToast(`发送失败：${error.message}`, 'error');
    }
  } finally {
    // 不论成功还是失败，只要后端已经分配过 message_id，就把它当作下一轮的 parent。
    // - 成功：正常线程化。
    // - 中途失败：后端可能已经写入了部分内容（或者已修的服务端逻辑判定为脏记录而跳过）。
    //   不存在的 parent 在 backend.buildHistory 里会被识别为空历史，副作用最小。
    if (state.activeMessageId) state.parentMessageId = state.activeMessageId;
    state.abortController = null;
    state.activeMessageId = '';
    setStreamingStatus(false);
    clearPendingStreamState();
    // 确保流式光标在中断/报错时也被清除
    elements.chatContainer?.querySelectorAll('.is-streaming').forEach((el) => el.classList.remove('is-streaming'));
  }
}

function clearCurrentChatView() {
  state.messages = [];
  state.parentMessageId = '';
  elements.chatContainer.innerHTML = '';
  resetPipelineState();
  hideHeroIfNeeded();
}

/* ============================ 模态预览 ============================ */

function openImagePreview(url) {
  elements.imagePreviewLarge.src = url;
  elements.imagePreviewModal.classList.remove('hidden');
}

function closeImagePreview() {
  elements.imagePreviewModal.classList.add('hidden');
  elements.imagePreviewLarge.src = '';
}

function openSlidePreview(svgContent) {
  // 调用方多数已经经过 renderTrustedSlideSVG，这里再过一次净化做兜底，
  // 避免任何忘记走清理路径的来源（例如未来新加的入口）直接把不可信 SVG 注入预览大图。
  const safe = sanitizeSVG(svgContent);
  if (!safe) return;
  elements.slidePreviewContent.innerHTML = safe;
  elements.slidePreviewModal.classList.remove('hidden');
}

function closeSlidePreview() {
  elements.slidePreviewModal.classList.add('hidden');
  elements.slidePreviewContent.innerHTML = '';
}

/* ============================ 事件绑定 ============================ */

function bindEvents() {
  elements.authForm?.addEventListener('submit', handleAuthSubmit);
  elements.authModeBtn?.addEventListener('click', () => {
    state.authMode = state.authMode === 'login' ? 'register' : 'login';
    renderAuthMode();
    elements.authError?.classList.add('hidden');
  });
  elements.logoutBtn?.addEventListener('click', () => {
    clearAuthSession(true);
    showAuthOverlay();
  });

  elements.sidebarToggleBtn?.addEventListener('click', () => setSidebarCollapsed(true));
  elements.sidebarExpandBtn?.addEventListener('click', () => setSidebarCollapsed(false));

  // 视图 tab
  elements.viewTabs.forEach((tab) => {
    tab.addEventListener('click', () => {
      if (tab.dataset.route === 'editor') {
        openStandaloneEditor();
        return;
      }
      setActiveView(tab.dataset.view);
    });
  });

  // 视图内的 close-x 与"返回对话"按钮
  document.body.addEventListener('click', (event) => {
    const back = event.target.closest('[data-back]');
    if (back) {
      setActiveView(back.dataset.back);
    }
  });

  // 模板 - 选中 / 生成
  elements.tplGrid?.addEventListener('click', (event) => {
    const thumb = event.target.closest('[data-template-name]');
    if (!thumb) return;
    state.selectedTemplateName = thumb.dataset.templateName;
    renderTemplateLibrary();
  });
  elements.tplGenerateBtn?.addEventListener('click', () => {
    const selected = getSelectedTemplate();
    const pending = state.pendingTemplateRequest;
    if (!selected || !pending?.text) {
      showToast('请先输入 PPT 需求后再选择模板', 'info');
      return;
    }
    const finalPrompt = `${pending.text}\n\n请直接使用模板「${selected.name}」（${selected.label || selected.name}）生成，不要再次进入模板选择步骤。`;
    const pendingUploads = [...(pending.uploads || [])];
    state.pendingTemplateRequest = null;
    clearTemplateRecommendation();
    state.selectedTemplateName = selected.name;
    state.uploads = [];
    renderUploads();
    handleSend(
      pending.text,
      pendingUploads,
      {
        displayText: `使用模板「${selected.label || selected.name}」生成`,
        backendText: finalPrompt,
        skipTemplateRecommendation: true,
      },
    );
  });
  elements.tplBackToChatBtn?.addEventListener('click', () => {
    setActiveView('chat');
    elements.messageInput?.focus();
  });

  // 聊天内 PPT 卡片
  elements.chatContainer?.addEventListener('click', (event) => {
    const openBtn = event.target.closest('[data-action="open-editor"]');
    if (openBtn) {
      if (openBtn.tagName === 'A' && openBtn.href) return;
      const projectName = openBtn.dataset.projectName
        || openBtn.closest('.chat-ppt-card')?.dataset.projectName
        || state.pptProject?.name
        || '';
      if (!projectName) {
        showToast('未找到 PPT 项目名，请刷新会话后重试', 'error');
        return;
      }
      if (!state.pptProject || state.pptProject.name !== projectName) {
        restoreProjectFromURL(projectName).finally(() => openStandaloneEditorInNewTab(projectName));
        return;
      }
      openStandaloneEditorInNewTab(projectName);
      return;
    }
    const previewBtn = event.target.closest('[data-action="preview-slide"]');
    if (previewBtn) {
      const svg = previewBtn.innerHTML;
      if (svg) openSlidePreview(svg);
      return;
    }
    const upload = event.target.closest('.inline-upload');
    if (upload) openImagePreview(upload.src);
  });

  // 右栏建议项 → 发送消息
  document.body.addEventListener('click', (event) => {
    const sug = event.target.closest('.rp-suggest[data-prompt]');
    if (sug) {
      handleSend(sug.dataset.prompt);
    }
  });

  // 会话相关
  document.addEventListener('click', (event) => {
    if (!event.target.closest('.session-item') && state.openSessionMenuId) {
      state.openSessionMenuId = null;
      renderSessionList();
    }
  });

  elements.sessionList?.addEventListener('click', async (event) => {
    const actionBtn = event.target.closest('[data-action]');
    if (actionBtn) {
      const { action, sessionId } = actionBtn.dataset;
      event.stopPropagation();
      if (action === 'toggle-menu') {
        state.openSessionMenuId = state.openSessionMenuId === sessionId ? null : sessionId;
        renderSessionList();
        return;
      }
      if (action === 'rename') await renameSession(sessionId);
      else if (action === 'delete') await deleteSession(sessionId);
      state.openSessionMenuId = null;
      renderSessionList();
      return;
    }
    const chip = event.target.closest('.session-chip');
    if (!chip) return;
    await switchSession(chip.dataset.sessionId);
  });

  elements.sessionList?.addEventListener('keydown', async (event) => {
    const chip = event.target.closest('.session-chip');
    if (!chip) return;
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      await switchSession(chip.dataset.sessionId);
    }
  });

  elements.newSessionBtn?.addEventListener('click', async () => {
    await createNewSession();
    elements.messageInput?.focus();
  });

  elements.sessionSearchInput?.addEventListener('input', (event) => {
    state.sessionSearchQuery = event.target.value.trim();
    renderSessionList();
  });

  // 输入框
  elements.messageInput?.addEventListener('input', () => {
    autoResizeTextarea();
    toggleSendBtn();
  });
  elements.messageInput?.addEventListener('keydown', (event) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      handleSend();
    }
  });
  elements.sendBtn?.addEventListener('click', () => handleSend());
  elements.stopBtn?.addEventListener('click', () => state.abortController?.abort());
  elements.clearChatBtn?.addEventListener('click', () => {
    clearCurrentChatView();
    state.pptProject = null;
    clearTemplateRecommendation();
    state.pendingTemplateRequest = null;
    renderRightRails();
    renderTemplateLibrary();
  });

  elements.suggestionGrid?.addEventListener('click', (event) => {
    const card = event.target.closest('.suggestion-card');
    if (!card) return;
    handleSend(card.dataset.prompt || '');
  });

  // 文件上传
  elements.imageInput?.addEventListener('change', async (event) => {
    const remain = Math.max(CONFIG.maxFileCount - state.uploads.length, 0);
    const files = Array.from(event.target.files || []).slice(0, remain);
    for (const file of files) {
      const upload = await parseUploadFile(file);
      state.uploads.push(upload);
    }
    renderUploads();
    event.target.value = '';
  });

  elements.uploadPreview?.addEventListener('click', (event) => {
    const btn = event.target.closest('.preview-remove');
    if (btn) {
      state.uploads = state.uploads.filter((item) => {
        if (item.id === btn.dataset.id) {
          if (item.url) URL.revokeObjectURL(item.url);
          return false;
        }
        return true;
      });
      renderUploads();
      return;
    }
    const card = event.target.closest('.preview-item');
    if (!card || card.dataset.kind !== 'image') return;
    const img = card.querySelector('img');
    if (img?.src) openImagePreview(img.src);
  });

  // 模态
  elements.imagePreviewClose?.addEventListener('click', closeImagePreview);
  elements.imagePreviewModal?.addEventListener('click', (event) => {
    if (event.target === elements.imagePreviewModal) closeImagePreview();
  });
  elements.slidePreviewClose?.addEventListener('click', closeSlidePreview);
  elements.slidePreviewModal?.addEventListener('click', (event) => {
    if (event.target === elements.slidePreviewModal) closeSlidePreview();
  });

  // 右栏输入框 → 发送
  document.body.addEventListener('keydown', (event) => {
    const input = event.target.closest('.rp-input-field');
    if (input && event.key === 'Enter') {
      event.preventDefault();
      const text = input.value.trim();
      if (text) {
        input.value = '';
        handleSend(text);
      }
    }
  });
  document.body.addEventListener('click', (event) => {
    const sendBtn = event.target.closest('.rp-send');
    if (!sendBtn) return;
    const input = sendBtn.parentElement.querySelector('.rp-input-field');
    const text = input?.value.trim();
    if (text) {
      input.value = '';
      handleSend(text);
    }
  });

  // 编辑器：双击右栏 PPT 卡片缩略图打开预览
  document.body.addEventListener('click', (event) => {
    const thumb = event.target.closest('.rp-ppt-thumb');
    if (!thumb) return;
    const svg = thumb.innerHTML;
    if (svg) openSlidePreview(svg);
  });

  document.addEventListener('keydown', (event) => {
    if (event.key === 'Escape') {
      closeImagePreview();
      closeSlidePreview();
    }
  });
}

/* ============================ 启动 ============================ */

async function bootstrap() {
  bindEvents();
  setSidebarCollapsed(state.sidebarCollapsed);
  autoResizeTextarea();
  toggleSendBtn();
  renderUploads();
  renderRightRails();
  updateAuthUI();
  setActiveView(state.activeView);

  if (!state.authToken) {
    showAuthOverlay();
    return;
  }

  try {
    await loadCurrentUser();
    hideAuthOverlay();
    await loadAppData();
  } catch (error) {
    showAuthOverlay(error.message || '请先登录');
  }
}

async function loadAppData() {
  try {
    await Promise.all([loadPPTTemplates(), loadConversations()]);
    if (state.activeSessionId) await loadConversationMessages(state.activeSessionId);
    await restoreProjectFromURL();
    hideHeroIfNeeded();
  } catch (error) {
    showToast(`初始化失败：${error.message}`, 'error');
  }
}

bootstrap().catch((error) => {
  console.error(error);
  showToast(`初始化失败：${error.message}`, 'error');
});
