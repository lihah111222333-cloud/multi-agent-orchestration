import { computed, nextTick, reactive, ref, watch } from '../../lib/vue.esm-browser.prod.js';
import { callAPI, selectProjectDirs } from '../services/api.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';
import { renderAssistantMarkdown } from '../utils/assistant-markdown.js';

function normalizeWordList(text) {
  const raw = (text || '').toString().trim();
  if (!raw) return [];
  const normalized = raw
    .replace(/[，、；;\n]/g, ',')
    .split(',')
    .map((item) => cleanScalar(item))
    .filter(Boolean);
  const dedup = [];
  const seen = new Set();
  for (const word of normalized) {
    const key = word.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    dedup.push(word);
  }
  return dedup;
}

function listToText(words) {
  if (!Array.isArray(words) || words.length === 0) return '';
  return words.join(', ');
}

function inferSkillNameFromPath(path) {
  const normalized = (path || '').toString().trim().replace(/[\\/]+$/g, '');
  if (!normalized) return '';
  const parts = normalized.split(/[\\/]/).filter(Boolean);
  if (parts.length === 0) return '';
  return parts[parts.length - 1].trim();
}

function summarizeItems(items, limit = 3) {
  if (!Array.isArray(items) || items.length === 0) return '';
  const visible = items.slice(0, limit);
  const remaining = items.length - visible.length;
  if (remaining <= 0) return visible.join(', ');
  return `${visible.join(', ')} 等 ${items.length} 项`;
}

function parseFrontmatter(content) {
  const text = (content || '').replace(/\r\n/g, '\n');
  if (!text.startsWith('---\n')) {
    return { attrs: {}, body: text };
  }
  const rest = text.slice(4);
  const end = rest.indexOf('\n---');
  if (end < 0) {
    return { attrs: {}, body: text };
  }
  const header = rest.slice(0, end);
  const body = rest.slice(end + 4).replace(/^\n/, '');
  const lines = header.split('\n');
  const attrs = {};
  for (let i = 0; i < lines.length; i += 1) {
    const line = (lines[i] || '').trim();
    if (!line || line.startsWith('#')) continue;
    const idx = line.indexOf(':');
    if (idx <= 0) continue;
    const key = line.slice(0, idx).trim().toLowerCase().replace(/-/g, '_');
    const value = line.slice(idx + 1).trim();
    if (value) {
      attrs[key] = value;
      continue;
    }
    const list = [];
    let consumed = 0;
    for (let j = i + 1; j < lines.length; j += 1) {
      const listLine = (lines[j] || '').trim();
      if (!listLine) {
        consumed += 1;
        continue;
      }
      if (!listLine.startsWith('-')) break;
      consumed += 1;
      list.push(listLine.slice(1).trim());
    }
    if (list.length > 0) {
      attrs[key] = list;
      i += consumed;
    }
  }
  return { attrs, body };
}

function parseWordsValue(value) {
  if (Array.isArray(value)) {
    return normalizeWordList(value.join(','));
  }
  const text = (value || '').toString().trim();
  if (!text) return [];
  if (text.startsWith('[') && text.endsWith(']')) {
    return normalizeWordList(text.slice(1, -1));
  }
  return normalizeWordList(text);
}

function cleanScalar(value) {
  return (value || '').toString().trim().replace(/^['"]|['"]$/g, '').trim();
}

function parseSkillMarkdown(content, fallbackName = '') {
  const { attrs, body } = parseFrontmatter(content);
  const name = cleanScalar(attrs.name) || fallbackName;
  const description = cleanScalar(attrs.description);
  const summary = cleanScalar(attrs.summary ?? attrs.digest ?? '');
  const triggerWords = normalizeWordList([
    ...parseWordsValue(
      attrs.trigger_words ?? attrs.triggerwords ?? attrs.trigger_words_list ?? attrs.triggers ?? '',
    ),
    ...parseWordsValue(
      attrs.aliases ?? attrs.alias ?? attrs.tags ?? attrs.tag ?? attrs.keywords ?? attrs.keyword ?? '',
    ),
  ].join(','));
  const forceWords = parseWordsValue(
    attrs.force_words ?? attrs.forcewords ?? attrs.mandatory_words ?? attrs.must_words ?? '',
  );
  return {
    name,
    description,
    summary,
    triggerWords,
    forceWords,
    body: body || '',
  };
}

function quoteYAML(value) {
  return `"${(value || '').replace(/"/g, '\\"')}"`;
}

function buildSkillMarkdown(form) {
  const name = (form.name || '').trim();
  const description = (form.description || '').trim();
  const summary = (form.summary || '').trim();
  const triggerWords = normalizeWordList(form.triggerWordsText);
  const forceWords = normalizeWordList(form.forceWordsText);
  const body = (form.body || '').toString().trim();
  const lines = ['---', `name: ${quoteYAML(name)}`];
  if (description) lines.push(`description: ${quoteYAML(description)}`);
  if (summary) lines.push(`summary: ${quoteYAML(summary)}`);
  if (triggerWords.length > 0) {
    lines.push(`trigger_words: [${triggerWords.map(quoteYAML).join(', ')}]`);
  }
  if (forceWords.length > 0) {
    lines.push(`force_words: [${forceWords.map(quoteYAML).join(', ')}]`);
  }
  lines.push('---', '', body || '## 说明\n\n请补充技能规则。');
  return lines.join('\n');
}

export const SkillsPage = {
  name: 'SkillsPage',
  props: {
    skills: { type: Array, default: () => [] },
  },
  emits: ['refresh-skills'],
  setup(props, { emit }) {
    const selectedSkillName = ref('');
    const summarySource = ref('');
    const sourcePath = ref('');
    const importFailures = ref([]);
    const notice = reactive({ level: 'info', message: '' });
    const saving = ref(false);
    const uploading = ref(false);
    const deletingSkillName = ref('');
    const searchQuery = ref('');
    const isEditorOpen = ref(false);
    const isBodyEditing = ref(false);
    const bodyEditorFocused = ref(false);
    const bodyInputRef = ref(null);

    const form = reactive({
      name: '',
      description: '',
      summary: '',
      triggerWordsText: '',
      forceWordsText: '',
      body: '',
    });

    const skillCards = computed(() => {
      const list = Array.isArray(props.skills) ? props.skills : [];
      return list.map((item) => ({
        name: (item?.name || '').toString(),
        dir: (item?.dir || '').toString(),
        description: (item?.description || '').toString(),
        summary: (item?.summary || item?.description || '').toString(),
        triggerWords: Array.isArray(item?.trigger_words) ? item.trigger_words : [],
        forceWords: Array.isArray(item?.force_words) ? item.force_words : [],
      }));
    });

    const filteredSkillCards = computed(() => {
      const keyword = (searchQuery.value || '').toString().trim().toLowerCase();
      if (!keyword) return skillCards.value;
      return skillCards.value.filter((item) => {
        const haystack = [
          item.name,
          item.description,
          item.summary,
          item.dir,
          ...(Array.isArray(item.triggerWords) ? item.triggerWords : []),
          ...(Array.isArray(item.forceWords) ? item.forceWords : []),
        ]
          .join(' ')
          .toLowerCase();
        return haystack.includes(keyword);
      });
    });

    const summarySourceLabel = computed(() => {
      const source = (summarySource.value || '').toLowerCase();
      if (source === 'frontmatter') return '用户摘要';
      if (source === 'description') return '系统生成（基于描述）';
      if (source === 'generated') return '系统生成（基于正文）';
      return '系统生成';
    });

    const skillBodyMarkdownHtml = computed(() => {
      const text = (form.body || '').toString().trim();
      if (!text) return '<p>暂无内容，点击“编辑正文”开始编写。</p>';
      return renderAssistantMarkdown(text);
    });

    function setNotice(level, message) {
      notice.level = level || 'info';
      notice.message = (message || '').toString();
    }

    function applyParsedSkill(parsed, rawContent, path = '', fallbackSummary = '', fallbackSource = '') {
      form.name = parsed.name || form.name || '';
      form.description = parsed.description || '';
      form.summary = parsed.summary || fallbackSummary || parsed.description || '';
      if (parsed.summary) {
        summarySource.value = 'frontmatter';
      } else if (fallbackSource) {
        summarySource.value = fallbackSource;
      } else if (fallbackSummary) {
        summarySource.value = 'generated';
      } else if (parsed.description) {
        summarySource.value = 'description';
      } else {
        summarySource.value = '';
      }
      form.triggerWordsText = listToText(parsed.triggerWords);
      form.forceWordsText = listToText(parsed.forceWords);
      form.body = (parsed.body || '').trim();
      sourcePath.value = path;
      selectedSkillName.value = form.name || selectedSkillName.value;
      logInfo('skills', 'editor.skill.loaded', {
        name: form.name,
        source_path: path,
        body_len: rawContent.length,
      });
    }

    async function readSkillFile(path, fallbackName = '', fallbackSummary = '', fallbackSource = '') {
      const raw = await callAPI('skills/local/read', { path });
      const content = (raw?.skill?.content || '').toString();
      if (!content.trim()) {
        throw new Error('读取的技能文件为空');
      }
      const serverSummary = (raw?.skill?.summary || '').toString().trim();
      const serverSummarySource = (raw?.skill?.summary_source || '').toString().trim();
      const parsed = parseSkillMarkdown(content, fallbackName);
      const finalFallbackSummary = serverSummary || fallbackSummary;
      const finalFallbackSource = serverSummarySource || fallbackSource;
      applyParsedSkill(parsed, content, path, finalFallbackSummary, finalFallbackSource);
      if (!parsed.summary && finalFallbackSummary) {
        setNotice('info', '系统已生成摘要，你可以在编辑后保存为自定义摘要。');
      }
    }

    async function onUploadSkill() {
      if (uploading.value) return;
      uploading.value = true;
      importFailures.value = [];
      try {
        const folderPaths = await selectProjectDirs();
        if (!Array.isArray(folderPaths) || folderPaths.length === 0) {
          setNotice('info', '未选择目录');
          return;
        }

        const selectedNames = folderPaths
          .map((path) => inferSkillNameFromPath(path))
          .filter(Boolean);
        const selectedNameSeen = new Set();
        const duplicatedNames = [];
        for (const name of selectedNames) {
          const key = name.toLowerCase();
          if (selectedNameSeen.has(key)) {
            if (!duplicatedNames.some((item) => item.toLowerCase() === key)) duplicatedNames.push(name);
            continue;
          }
          selectedNameSeen.add(key);
        }
        if (duplicatedNames.length > 0) {
          setNotice('error', `选择目录中存在重复技能名：${summarizeItems(duplicatedNames)}`);
          return;
        }

        const existingNameSet = new Set(
          skillCards.value.map((item) => (item?.name || '').toString().toLowerCase()).filter(Boolean),
        );
        const overwriteNames = selectedNames.filter((name) => existingNameSet.has(name.toLowerCase()));
        if (overwriteNames.length > 0) {
          setNotice('info', `将覆盖已有技能：${summarizeItems(overwriteNames)}，继续导入中...`);
        }

        const imported = await callAPI('skills/local/importDir', { paths: folderPaths });
        const importedSkills = Array.isArray(imported?.skills) ? imported.skills : [];
        const failures = Array.isArray(imported?.failures) ? imported.failures : [];
        importFailures.value = failures.map((item) => {
          const source = (item?.source || '').toString().trim();
          const message = (item?.error || '未知错误').toString().trim();
          return `${source || '-'}：${message || '未知错误'}`;
        });
        const firstSkill = importedSkills[0] || null;

        emit('refresh-skills');
        if (firstSkill?.skill_file) {
          await readSkillFile(firstSkill.skill_file, firstSkill.name || '');
        }
        if (failures.length > 0) {
          setNotice('error', `导入完成：成功 ${importedSkills.length}，失败 ${failures.length}`);
          return;
        }
        if (importedSkills.length === 0) {
          setNotice('info', '未导入任何技能目录');
          return;
        }
        setNotice('success', `已导入 ${importedSkills.length} 个技能目录（含资源文件）`);
      } catch (error) {
        logWarn('skills', 'upload.failed', { error });
        setNotice('error', `导入目录失败：${error?.message || error}`);
      } finally {
        uploading.value = false;
      }
    }

    function onCreateSkill() {
      selectedSkillName.value = '';
      summarySource.value = '';
      sourcePath.value = '';
      form.name = '';
      form.description = '';
      form.summary = '';
      form.triggerWordsText = '';
      form.forceWordsText = '';
      form.body = '';
      isBodyEditing.value = true;
      bodyEditorFocused.value = false;
      isEditorOpen.value = true;
      setNotice('info', '已打开新建表单，填写后点击保存。');
      nextTick(() => {
        const node = bodyInputRef.value;
        if (node && typeof node.focus === 'function') node.focus();
      });
    }

    async function onEditSkill(item) {
      if (!item?.dir) return;
      const skillPath = `${item.dir}/SKILL.md`;
      try {
        await readSkillFile(skillPath, item.name || '', item.summary || '', item.summary ? 'generated' : '');
        selectedSkillName.value = item.name || '';
        isBodyEditing.value = false;
        bodyEditorFocused.value = false;
        isEditorOpen.value = true;
        setNotice('info', `已加载技能：${item.name || ''}`);
      } catch (error) {
        logWarn('skills', 'load.savedSkill.failed', { error, path: skillPath });
        setNotice('error', `读取技能失败：${error?.message || error}`);
      }
    }

    function isDeletingSkill(name) {
      return (deletingSkillName.value || '').toLowerCase() === (name || '').toString().toLowerCase();
    }

    async function onDeleteSkill(item) {
      const skillName = (item?.name || '').toString().trim();
      if (!skillName || deletingSkillName.value) return;
      const confirmed = typeof window === 'undefined' || typeof window.confirm !== 'function'
        ? true
        : window.confirm(`确定删除技能 "${skillName}" 吗？\n该操作会删除技能目录及其资源文件。`);
      if (!confirmed) return;
      deletingSkillName.value = skillName;
      try {
        await callAPI('skills/local/delete', { name: skillName });
        const skillKey = skillName.toLowerCase();
        if ((selectedSkillName.value || '').toLowerCase() === skillKey) {
          selectedSkillName.value = '';
        }
        if ((form.name || '').toLowerCase() === skillKey) {
          form.name = '';
          form.description = '';
          form.summary = '';
          form.triggerWordsText = '';
          form.forceWordsText = '';
          form.body = '';
          summarySource.value = '';
          sourcePath.value = '';
        }
        if (!selectedSkillName.value) {
          isEditorOpen.value = false;
        }
        emit('refresh-skills');
        setNotice('success', `技能已删除：${skillName}`);
      } catch (error) {
        logWarn('skills', 'delete.failed', { error, skill: skillName });
        setNotice('error', `删除技能失败：${error?.message || error}`);
      } finally {
        deletingSkillName.value = '';
      }
    }

    function closeEditor() {
      isEditorOpen.value = false;
      isBodyEditing.value = false;
      bodyEditorFocused.value = false;
    }

    function startBodyEdit() {
      isBodyEditing.value = true;
      nextTick(() => {
        const node = bodyInputRef.value;
        if (node && typeof node.focus === 'function') node.focus();
      });
    }

    function finishBodyEdit() {
      isBodyEditing.value = false;
      bodyEditorFocused.value = false;
    }

    function onBodyFocus() {
      bodyEditorFocused.value = true;
    }

    function onBodyBlur() {
      bodyEditorFocused.value = false;
    }

    async function onSaveSkill() {
      const name = (form.name || '').trim();
      if (!name) {
        setNotice('error', '请先填写技能名称');
        return;
      }
      saving.value = true;
      try {
        const content = buildSkillMarkdown(form);
        await callAPI('skills/config/write', { name, content });
        selectedSkillName.value = name;
        summarySource.value = 'frontmatter';
        emit('refresh-skills');
        setNotice('success', `技能已保存：${name}`);
      } catch (error) {
        logWarn('skills', 'save.failed', { error, name });
        setNotice('error', `保存失败：${error?.message || error}`);
      } finally {
        saving.value = false;
      }
    }

    watch(skillCards, (nextCards) => {
      const current = selectedSkillName.value;
      if (!current) return;
      const exists = nextCards.some((item) => item.name.toLowerCase() === current.toLowerCase());
      if (!exists) {
        selectedSkillName.value = '';
        isEditorOpen.value = false;
      }
    });

    logDebug('skills', 'page.ready', {});

    return {
      selectedSkillName,
      summarySource,
      summarySourceLabel,
      sourcePath,
      importFailures,
      notice,
      saving,
      uploading,
      deletingSkillName,
      searchQuery,
      filteredSkillCards,
      isEditorOpen,
      isBodyEditing,
      bodyEditorFocused,
      bodyInputRef,
      form,
      skillCards,
      onUploadSkill,
      onCreateSkill,
      onEditSkill,
      onSaveSkill,
      skillBodyMarkdownHtml,
      closeEditor,
      startBodyEdit,
      finishBodyEdit,
      onBodyFocus,
      onBodyBlur,
      isDeletingSkill,
      onDeleteSkill,
    };
  },
  template: `
    <section id="page-skills" class="page active skills-page" data-testid="skills-page">
      <div class="panel-header">
        <div class="ph-bar"></div>
        <div class="ph-text"><h2>技能管理</h2></div>
      </div>
      <div class="split-duo" data-testid="skills-split">
        <div class="split-left" data-testid="skills-left">
          <div class="section-header">SKILL 列表</div>
          <div class="panel-body skills-list-panel" data-testid="skills-list-panel">
            <div class="skills-toolbar" data-testid="skills-toolbar">
              <button class="btn btn-secondary" data-testid="skills-import-button" :disabled="uploading" @click="onUploadSkill">
                {{ uploading ? '导入中...' : '批量导入技能目录' }}
              </button>
              <button class="btn btn-ghost" data-testid="skills-create-button" @click="onCreateSkill">
                新建 Skill
              </button>
              <div class="skills-search-wrap">
                <input
                  v-model="searchQuery"
                  class="modal-input skills-search-input"
                  data-testid="skills-search-input"
                  placeholder="搜索技能名称、摘要、触发词..."
                />
              </div>
            </div>
            <div v-if="skillCards.length === 0" class="empty-state" data-testid="skills-empty-state">
              <div class="es-icon">S</div>
              <h3>暂无 Skill</h3>
              <p>支持一次导入多个目录（每个目录需包含 SKILL.md）</p>
            </div>
            <div v-else-if="filteredSkillCards.length === 0" class="empty-state" data-testid="skills-search-empty-state">
              <div class="es-icon">?</div>
              <h3>没有匹配技能</h3>
              <p>尝试更换关键词，支持按名称、描述、摘要、触发词搜索</p>
            </div>
            <div v-else class="skills-card-grid" data-testid="skills-list">
              <article
                v-for="(item, idx) in filteredSkillCards"
                :key="item.name"
                class="data-card-vue skill-card skill-card-compact"
                :class="{ active: selectedSkillName.toLowerCase() === item.name.toLowerCase() }"
                :data-testid="'skills-card-' + idx"
              >
                <div class="skill-card-header">
                  <div class="skill-card-heading">
                    <div class="skill-card-title">{{ item.name }}</div>
                    <div class="skill-card-path" :title="item.dir">{{ item.dir || '-' }}</div>
                  </div>
                  <span v-if="selectedSkillName.toLowerCase() === item.name.toLowerCase()" class="skill-card-badge">编辑中</span>
                </div>
                <div class="skill-card-description">{{ item.description || '暂无描述' }}</div>
                <div class="skill-card-summary-preview">{{ item.summary || '暂无摘要，点击编辑补充。' }}</div>
                <div class="skill-word-groups">
                  <div v-if="(item.triggerWords || []).length > 0" class="skill-word-line">
                    <strong>触发词</strong>
                    <div class="skill-chip-row">
                      <span
                        v-for="(word, wordIdx) in item.triggerWords.slice(0, 4)"
                        :key="'trigger-' + idx + '-' + wordIdx"
                        class="skill-word-chip"
                      >
                        {{ word }}
                      </span>
                      <span v-if="item.triggerWords.length > 4" class="skill-word-chip muted">+{{ item.triggerWords.length - 4 }}</span>
                    </div>
                  </div>
                  <div v-if="(item.forceWords || []).length > 0" class="skill-word-line">
                    <strong>强制词</strong>
                    <div class="skill-chip-row">
                      <span
                        v-for="(word, wordIdx) in item.forceWords.slice(0, 3)"
                        :key="'force-' + idx + '-' + wordIdx"
                        class="skill-word-chip skill-word-chip-force"
                      >
                        {{ word }}
                      </span>
                      <span v-if="item.forceWords.length > 3" class="skill-word-chip muted">+{{ item.forceWords.length - 3 }}</span>
                    </div>
                  </div>
                </div>
                <div class="data-actions-vue skill-actions">
                  <button class="btn btn-secondary btn-xs" :data-testid="'skills-edit-button-' + idx" @click="onEditSkill(item)">编辑详情</button>
                  <button class="btn btn-ghost btn-xs btn-warning" :data-testid="'skills-delete-button-' + idx" :disabled="Boolean(deletingSkillName)" @click="onDeleteSkill(item)">
                    {{ isDeletingSkill(item.name) ? '删除中...' : '删除' }}
                  </button>
                </div>
              </article>
            </div>
            <div v-if="skillCards.length > 0" class="skills-inline-tip">
              显示 {{ filteredSkillCards.length }} / {{ skillCards.length }} 个技能
            </div>
            <div v-if="notice.message" class="skills-notice" data-testid="skills-notice" :class="'is-' + notice.level">
              {{ notice.message }}
            </div>
            <ul v-if="importFailures.length > 0" class="skills-failure-list" data-testid="skills-failure-list">
              <li v-for="item in importFailures.slice(0, 5)" :key="item">{{ item }}</li>
            </ul>
            <div v-if="importFailures.length > 5" class="skills-inline-tip">
              还有 {{ importFailures.length - 5 }} 条失败项
            </div>
          </div>
        </div>
      </div>
      <div
        v-if="isEditorOpen"
        class="modal-overlay skills-editor-overlay"
        data-testid="skills-editor-modal-overlay"
        tabindex="0"
        @click.self="closeEditor"
        @keydown.esc.prevent="closeEditor"
      >
        <div class="modal-box skills-editor-modal" :class="{ 'is-body-expanded': isBodyEditing || bodyEditorFocused }" role="dialog" aria-modal="true" data-testid="skills-editor-panel">
          <div class="skills-editor-modal-head">
            <div>
              <div class="modal-title">编辑技能</div>
              <div class="skills-inline-tip">系统会先生成一版摘要；你可以直接修改并保存到 SKILL.md 的 frontmatter。</div>
            </div>
            <button class="btn btn-ghost" data-testid="skills-editor-close-button" @click="closeEditor">关闭</button>
          </div>
          <div class="skills-editor-panel">
            <div class="skills-field">
              <label>技能名称</label>
              <input v-model="form.name" class="modal-input" data-testid="skills-editor-name-input" placeholder="例如：backend" />
            </div>
            <div class="skills-field">
              <label>描述（可选）</label>
              <input v-model="form.description" class="modal-input" data-testid="skills-editor-description-input" placeholder="一句话描述" />
            </div>
            <div class="skills-field">
              <label>摘要（注入内容）</label>
              <textarea v-model="form.summary" class="modal-input" data-testid="skills-editor-summary-input" rows="3" placeholder="用于运行时注入的摘要，建议 1-3 句"></textarea>
              <div class="skills-inline-tip">摘要来源：{{ summarySourceLabel }}</div>
            </div>
            <div class="skills-field two-col">
              <div>
                <label>触发词（逗号分隔）</label>
                <input v-model="form.triggerWordsText" class="modal-input" data-testid="skills-editor-trigger-input" placeholder="api, golang, backend" />
              </div>
              <div>
                <label>强制词（逗号分隔）</label>
                <input v-model="form.forceWordsText" class="modal-input" data-testid="skills-editor-force-input" placeholder="紧急, 必须, 强制" />
              </div>
            </div>
            <div class="skills-field skills-field-body">
              <div class="skills-body-head">
                <label>SKILL 内容（默认自动解析 MD，可手动编辑）</label>
                <div class="skills-body-head-actions">
                  <button
                    v-if="!isBodyEditing"
                    class="btn btn-secondary btn-xs skills-body-toggle"
                    data-testid="skills-editor-body-edit-button"
                    @click="startBodyEdit"
                  >
                    编辑正文
                  </button>
                  <button
                    v-else
                    class="btn btn-ghost btn-xs skills-body-toggle"
                    data-testid="skills-editor-body-preview-button"
                    @click="finishBodyEdit"
                  >
                    预览正文
                  </button>
                </div>
              </div>
              <div
                v-if="!isBodyEditing"
                class="skills-body-preview chat-item-markdown codex-markdown-root"
                data-testid="skills-editor-body-preview"
                v-html="skillBodyMarkdownHtml"
              ></div>
              <textarea
                v-else
                ref="bodyInputRef"
                v-model="form.body"
                class="modal-input skills-body-input"
                :class="{ 'is-expanded': isBodyEditing || bodyEditorFocused }"
                data-testid="skills-editor-body-input"
                placeholder="输入技能正文 Markdown"
                @focus="onBodyFocus"
                @blur="onBodyBlur"
              ></textarea>
              <div class="skills-inline-tip">点击“编辑正文”进入放大编辑区；切回“预览正文”查看 Markdown 渲染效果。</div>
              <div v-if="sourcePath" class="skills-inline-tip">来源文件：{{ sourcePath }}</div>
            </div>
            <div class="skills-actions-row" data-testid="skills-editor-actions">
              <button class="btn btn-ghost" data-testid="skills-editor-cancel-button" @click="closeEditor">取消</button>
              <button class="btn btn-primary skills-save-btn" data-testid="skills-save-button" :disabled="saving" @click="onSaveSkill">
                {{ saving ? '保存中...' : '保存 Skill' }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </section>
  `,
};
