import { computed, reactive, ref, watch } from '../../lib/vue.esm-browser.prod.js';
import { callAPI, selectProjectDirs } from '../services/api.js';
import { logDebug, logInfo, logWarn } from '../services/log.js';

function normalizeWordList(text) {
  const raw = (text || '').toString().trim();
  if (!raw) return [];
  const normalized = raw
    .replace(/[，、；;\n]/g, ',')
    .split(',')
    .map((item) => item.trim())
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
    const key = line.slice(0, idx).trim().toLowerCase();
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
  const triggerWords = parseWordsValue(
    attrs.trigger_words ?? attrs.triggerwords ?? attrs.triggers ?? '',
  );
  const forceWords = parseWordsValue(
    attrs.force_words ?? attrs.forcewords ?? attrs.mandatory_words ?? '',
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
    threadStore: { type: Object, required: true },
  },
  emits: ['refresh-skills'],
  setup(props, { emit }) {
    const selectedSkillName = ref('');
    const selectedThreadId = ref('');
    const threadSkills = ref([]);
    const summarySource = ref('');
    const sourcePath = ref('');
    const importFailures = ref([]);
    const notice = reactive({ level: 'info', message: '' });
    const saving = ref(false);
    const uploading = ref(false);
    const binding = ref(false);
    const deletingSkillName = ref('');
    const inlineSummarySkillName = ref('');
    const inlineSummaryDraft = ref('');
    const inlineSummarySavingName = ref('');

    const form = reactive({
      name: '',
      description: '',
      summary: '',
      triggerWordsText: '',
      forceWordsText: '',
      body: '',
    });

    const threadOptions = computed(() => {
      const list = props.threadStore?.state?.threads;
      if (!Array.isArray(list)) return [];
      return list.map((item) => ({
        id: (item?.id || '').toString(),
        name: (item?.name || item?.id || '').toString(),
      })).filter((item) => item.id);
    });

    const activeThreadId = computed(() => (props.threadStore?.state?.activeThreadId || '').toString());

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

    const currentSkillInThread = computed(() => {
      const target = (form.name || '').trim().toLowerCase();
      if (!target) return false;
      return threadSkills.value.some((item) => item.toLowerCase() === target);
    });

    const summarySourceLabel = computed(() => {
      const source = (summarySource.value || '').toLowerCase();
      if (source === 'frontmatter') return '用户摘要';
      if (source === 'description') return '系统生成（基于描述）';
      if (source === 'generated') return '系统生成（基于正文）';
      return '系统生成';
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

    async function onEditSkill(item) {
      if (!item?.dir) return;
      cancelInlineSummaryEdit();
      const skillPath = `${item.dir}/SKILL.md`;
      try {
        await readSkillFile(skillPath, item.name || '', item.summary || '', item.summary ? 'generated' : '');
        selectedSkillName.value = item.name || '';
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
      const confirmed = typeof window === 'undefined'
        ? true
        : window.confirm(`确定删除技能 "${skillName}" 吗？\n该操作会删除技能目录及其资源文件。`);
      if (!confirmed) return;
      deletingSkillName.value = skillName;
      try {
        await callAPI('skills/local/delete', { name: skillName });
        const skillKey = skillName.toLowerCase();
        threadSkills.value = threadSkills.value
          .map((itemName) => (itemName || '').toString().trim())
          .filter((itemName) => itemName && itemName.toLowerCase() !== skillKey);
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
        cancelInlineSummaryEdit();
        emit('refresh-skills');
        setNotice('success', `技能已删除：${skillName}`);
      } catch (error) {
        logWarn('skills', 'delete.failed', { error, skill: skillName });
        setNotice('error', `删除技能失败：${error?.message || error}`);
      } finally {
        deletingSkillName.value = '';
      }
    }

    function isInlineSummaryEditing(name) {
      return (inlineSummarySkillName.value || '').toLowerCase() === (name || '').toString().toLowerCase();
    }

    function isInlineSummarySaving(name) {
      return (inlineSummarySavingName.value || '').toLowerCase() === (name || '').toString().toLowerCase();
    }

    async function startInlineSummaryEdit(item) {
      if (!item?.name || !item?.dir) return;
      cancelInlineSummaryEdit();
      const skillPath = `${item.dir}/SKILL.md`;
      try {
        await readSkillFile(skillPath, item.name || '', item.summary || '', item.summary ? 'generated' : '');
        selectedSkillName.value = item.name || '';
      } catch (error) {
        logWarn('skills', 'summary.inline.load.failed', { error, path: skillPath });
        setNotice('error', `读取技能失败：${error?.message || error}`);
        return;
      }
      inlineSummarySkillName.value = item.name;
      inlineSummaryDraft.value = (form.summary || item.summary || '').toString();
      setNotice('info', `正在编辑摘要：${item.name}`);
    }

    function cancelInlineSummaryEdit() {
      inlineSummarySkillName.value = '';
      inlineSummaryDraft.value = '';
    }

    async function saveInlineSummary(item) {
      if (!item?.name || !item?.dir || inlineSummarySavingName.value) return;
      const skillName = (item.name || '').toString().trim();
      if (!skillName) return;
      const nextSummary = (inlineSummaryDraft.value || '').toString().trim();
      const currentSummary = (item.summary || '').toString().trim();
      if (nextSummary === currentSummary) {
        cancelInlineSummaryEdit();
        return;
      }
      inlineSummarySavingName.value = skillName;
      try {
        await callAPI('skills/summary/write', {
          name: skillName,
          summary: nextSummary,
        });
        if ((form.name || '').toLowerCase() === skillName.toLowerCase()) {
          form.summary = nextSummary;
          summarySource.value = nextSummary ? 'frontmatter' : 'generated';
        }
        cancelInlineSummaryEdit();
        emit('refresh-skills');
        setNotice('success', `摘要已保存：${skillName}`);
      } catch (error) {
        logWarn('skills', 'summary.inline.save.failed', { error, skill: skillName });
        setNotice('error', `摘要保存失败：${error?.message || error}`);
      } finally {
        inlineSummarySavingName.value = '';
      }
    }

    function onInlineSummaryKeydown(event, item) {
      const key = (event?.key || '').toLowerCase();
      if (key === 'escape') {
        event.preventDefault();
        cancelInlineSummaryEdit();
        return;
      }
      if (key === 'enter' && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        saveInlineSummary(item);
      }
    }

    async function loadThreadSkills(options = {}) {
      const silent = Boolean(options?.silent);
      let threadId = (selectedThreadId.value || '').trim();
      if (!threadId) {
        const active = (activeThreadId.value || '').trim();
        if (active) {
          selectedThreadId.value = active;
          threadId = active;
        }
      }
      if (!threadId) {
        threadSkills.value = [];
        if (!silent) {
          setNotice('info', '请先选择会话，再刷新绑定');
        }
        return;
      }
      try {
        const raw = await callAPI('skills/config/read', { agent_id: threadId });
        threadSkills.value = Array.isArray(raw?.skills) ? raw.skills : [];
        if (!silent) {
          setNotice('success', `会话绑定已刷新（${threadSkills.value.length} 个技能）`);
        }
      } catch (error) {
        logWarn('skills', 'threadSkills.load.failed', { thread_id: threadId, error });
        setNotice('error', `读取会话技能失败：${error?.message || error}`);
      }
    }

    async function saveThreadSkills(nextSkills) {
      const threadId = (selectedThreadId.value || '').trim();
      if (!threadId) return;
      binding.value = true;
      try {
        const normalized = normalizeWordList((nextSkills || []).join(','));
        await callAPI('skills/config/write', {
          agent_id: threadId,
          skills: normalized,
        });
        threadSkills.value = normalized;
      } catch (error) {
        logWarn('skills', 'threadSkills.save.failed', { thread_id: threadId, error });
        setNotice('error', `设置会话技能失败：${error?.message || error}`);
      } finally {
        binding.value = false;
      }
    }

    async function toggleThreadSkill(skillName) {
      const name = (skillName || '').trim();
      if (!name) return;
      const exists = threadSkills.value.some((item) => item.toLowerCase() === name.toLowerCase());
      const next = exists
        ? threadSkills.value.filter((item) => item.toLowerCase() !== name.toLowerCase())
        : [...threadSkills.value, name];
      await saveThreadSkills(next);
      setNotice('success', exists ? `已从会话移除 ${name}` : `已加入会话技能 ${name}`);
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

    watch(activeThreadId, (next) => {
      if (!selectedThreadId.value && next) {
        selectedThreadId.value = next;
      }
    }, { immediate: true });

    watch(selectedThreadId, () => {
      loadThreadSkills({ silent: true }).catch(() => { });
    }, { immediate: true });

    watch(skillCards, (nextCards) => {
      const current = selectedSkillName.value;
      if (!current) return;
      const exists = nextCards.some((item) => item.name.toLowerCase() === current.toLowerCase());
      if (!exists) {
        selectedSkillName.value = '';
      }
      const inlineEditingName = inlineSummarySkillName.value;
      if (inlineEditingName) {
        const inlineExists = nextCards.some((item) => item.name.toLowerCase() === inlineEditingName.toLowerCase());
        if (!inlineExists) {
          cancelInlineSummaryEdit();
        }
      }
    });

    logDebug('skills', 'page.ready', {});

    return {
      selectedSkillName,
      selectedThreadId,
      threadSkills,
      summarySource,
      summarySourceLabel,
      sourcePath,
      importFailures,
      notice,
      saving,
      uploading,
      binding,
      deletingSkillName,
      form,
      threadOptions,
      skillCards,
      currentSkillInThread,
      onUploadSkill,
      onEditSkill,
      onSaveSkill,
      inlineSummaryDraft,
      isInlineSummaryEditing,
      isInlineSummarySaving,
      startInlineSummaryEdit,
      cancelInlineSummaryEdit,
      saveInlineSummary,
      onInlineSummaryKeydown,
      isDeletingSkill,
      onDeleteSkill,
      toggleThreadSkill,
      loadThreadSkills,
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
              <button class="btn btn-ghost" data-testid="skills-refresh-binding-button" @click="loadThreadSkills">刷新会话绑定</button>
            </div>
            <div v-if="skillCards.length === 0" class="empty-state" data-testid="skills-empty-state">
              <div class="es-icon">S</div>
              <h3>暂无 Skill</h3>
              <p>支持一次导入多个目录（每个目录需包含 SKILL.md）</p>
            </div>
            <div v-else class="data-list-vue" data-testid="skills-list">
              <article
                v-for="(item, idx) in skillCards"
                :key="item.name"
                class="data-card-vue skill-card"
                :class="{ active: selectedSkillName.toLowerCase() === item.name.toLowerCase() }"
                :data-testid="'skills-card-' + idx"
              >
                <div class="data-row-vue">
                  <strong>技能</strong>
                  <span>{{ item.name }}</span>
                </div>
                <div class="data-row-vue">
                  <strong>描述</strong>
                  <span>{{ item.description || '-' }}</span>
                </div>
                <div class="data-row-vue skill-summary-row">
                  <strong>摘要</strong>
                  <div class="skill-summary-cell">
                    <template v-if="isInlineSummaryEditing(item.name)">
                      <textarea
                        v-model="inlineSummaryDraft"
                        class="modal-input skill-summary-input"
                        rows="3"
                        placeholder="用于运行时注入的摘要，建议 1-3 句"
                        @keydown="onInlineSummaryKeydown($event, item)"
                      ></textarea>
                      <div class="skill-summary-actions">
                        <button class="btn btn-primary btn-xs" :data-testid="'skills-summary-save-button-' + idx" :disabled="isInlineSummarySaving(item.name)" @click="saveInlineSummary(item)">
                          {{ isInlineSummarySaving(item.name) ? '保存中...' : '保存' }}
                        </button>
                        <button class="btn btn-ghost btn-xs" :data-testid="'skills-summary-cancel-button-' + idx" :disabled="isInlineSummarySaving(item.name)" @click="cancelInlineSummaryEdit">
                          取消
                        </button>
                      </div>
                      <div class="skills-inline-tip">Cmd/Ctrl + Enter 保存，Esc 取消</div>
                    </template>
                    <button
                      v-else
                      type="button"
                      class="skill-summary-text"
                      :title="'点击编辑摘要：' + item.name"
                      @click="startInlineSummaryEdit(item)"
                    >
                      {{ item.summary || '点击添加摘要' }}
                    </button>
                  </div>
                </div>
                <div class="data-row-vue">
                  <strong>触发词</strong>
                  <span>{{ (item.triggerWords || []).join(', ') || '-' }}</span>
                </div>
                <div class="data-row-vue">
                  <strong>强制词</strong>
                  <span>{{ (item.forceWords || []).join(', ') || '-' }}</span>
                </div>
                <div class="data-actions-vue skill-actions">
                  <button class="btn btn-ghost btn-xs" :data-testid="'skills-edit-button-' + idx" @click="onEditSkill(item)">编辑详情</button>
                  <button class="btn btn-ghost btn-xs" :data-testid="'skills-toggle-thread-button-' + idx" :disabled="binding" @click="toggleThreadSkill(item.name)">
                    {{ threadSkills.some((s) => s.toLowerCase() === item.name.toLowerCase()) ? '移出会话' : '加入会话' }}
                  </button>
                  <button class="btn btn-ghost btn-xs btn-warning" :data-testid="'skills-delete-button-' + idx" :disabled="Boolean(deletingSkillName)" @click="onDeleteSkill(item)">
                    {{ isDeletingSkill(item.name) ? '删除中...' : '删除' }}
                  </button>
                </div>
              </article>
            </div>
          </div>
        </div>
        <div class="split-divider"></div>
        <div class="split-right" data-testid="skills-editor-right">
          <div class="section-header">编辑器</div>
          <div class="panel-body skills-editor-panel" data-testid="skills-editor-panel">
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
              <div class="skills-inline-tip">系统会先生成一版摘要；你可以直接修改并保存到 SKILL.md 的 frontmatter。</div>
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
            <div class="skills-field">
              <label>绑定会话</label>
              <select v-model="selectedThreadId" class="project-selector" data-testid="skills-editor-thread-select">
                <option value="">未选择会话</option>
                <option v-for="item in threadOptions" :key="item.id" :value="item.id">
                  {{ item.name }} ({{ item.id }})
                </option>
              </select>
              <div class="skills-inline-tip">
                当前技能{{ currentSkillInThread ? '已' : '未' }}加入该会话
              </div>
            </div>
            <div class="skills-field">
              <label>SKILL 内容（默认自动解析 MD，可手动编辑）</label>
              <textarea v-model="form.body" class="modal-input skills-body-input" data-testid="skills-editor-body-input" placeholder="输入技能正文 Markdown"></textarea>
              <div v-if="sourcePath" class="skills-inline-tip">来源文件：{{ sourcePath }}</div>
            </div>
            <div class="skills-actions-row" data-testid="skills-editor-actions">
              <button class="btn btn-primary" data-testid="skills-save-button" :disabled="saving" @click="onSaveSkill">
                {{ saving ? '保存中...' : '保存 Skill' }}
              </button>
              <button class="btn btn-secondary" data-testid="skills-toggle-current-thread-button" :disabled="binding || !form.name" @click="toggleThreadSkill(form.name)">
                {{ currentSkillInThread ? '从当前会话移除' : '加入当前会话' }}
              </button>
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
    </section>
  `,
};
