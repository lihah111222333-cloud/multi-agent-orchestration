/**
 * JsonRenderWidgets — json-render 组件注册表。
 * 每个 widget 定义了一个 Vue 组件，用于渲染特定 type 的 spec。
 */
import { h, defineComponent, ref, onMounted, onBeforeUnmount, watch, nextTick } from '../../lib/vue.esm-browser.prod.js';

// ──── 递归渲染子 spec 的辅助函数 ────
function renderChildren(children, JsonRenderer) {
    if (!Array.isArray(children)) return [];
    return children.map((child, i) => {
        if (typeof child === 'string') return child;
        if (child && typeof child === 'object' && child.type) {
            return h(JsonRenderer, { key: i, spec: child });
        }
        return null;
    });
}

// 懒加载 JsonRenderer 避免循环依赖
let _JsonRenderer = null;
function getRenderer() {
    if (!_JsonRenderer) {
        // 延迟 import 打破循环
        _JsonRenderer = defineComponent({
            name: 'JrChildRenderer',
            props: { spec: Object },
            setup(props) {
                return () => {
                    const entry = WIDGET_REGISTRY[(props.spec?.type || '')];
                    if (!entry) return h('span', { class: 'jr-unknown' }, `Unknown: ${props.spec?.type}`);
                    return h(entry.component, { spec: props.spec });
                };
            },
        });
    }
    return _JsonRenderer;
}

// ──── Widget 组件定义 ────

const JrCard = defineComponent({
    name: 'JrCard',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const header = (s.title || s.description)
                ? h('div', { class: 'jr-card-header' }, [
                    s.title ? h('h3', { class: 'jr-card-title' }, s.title) : null,
                    s.description ? h('p', { class: 'jr-card-desc' }, s.description) : null,
                ])
                : null;
            const body = h('div', { class: 'jr-card-body' },
                renderChildren(s.children || [], getRenderer()));
            return h('div', { class: 'jr-root jr-card' }, [header, body]);
        };
    },
});

const JrMetric = defineComponent({
    name: 'JrMetric',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            return h('div', { class: 'jr-root jr-metric' }, [
                h('span', { class: 'jr-metric-label' }, s.label || ''),
                h('span', { class: 'jr-metric-value' }, String(s.value ?? '')),
            ]);
        };
    },
});

const JrStack = defineComponent({
    name: 'JrStack',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const dir = s.direction === 'row' ? 'jr-stack-row' : 'jr-stack-col';
            const gap = s.gap ? `${s.gap}px` : '8px';
            return h('div', {
                class: `jr-root jr-stack ${dir}`,
                style: { gap },
            }, renderChildren(s.children || [], getRenderer()));
        };
    },
});

const JrHeading = defineComponent({
    name: 'JrHeading',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const level = Math.min(Math.max(Number(s.level) || 2, 1), 4);
            const tag = `h${level}`;
            return h(tag, { class: 'jr-root jr-heading' }, s.text || '');
        };
    },
});

const JrTable = defineComponent({
    name: 'JrTable',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const cols = s.columns || [];
            const rows = s.rows || [];
            return h('div', { class: 'jr-root jr-table-wrap' }, [
                h('table', { class: 'jr-table' }, [
                    h('thead', [
                        h('tr', cols.map(c =>
                            h('th', { class: 'jr-table-th' }, typeof c === 'string' ? c : (c.label || c.key || ''))
                        )),
                    ]),
                    h('tbody', rows.map((row, ri) =>
                        h('tr', { key: ri }, cols.map((c, ci) => {
                            const key = typeof c === 'string' ? c : (c.key || c.label || '');
                            const val = Array.isArray(row) ? row[ci] : (row[key] ?? '');
                            return h('td', { class: 'jr-table-td' }, String(val));
                        }))
                    )),
                ]),
            ]);
        };
    },
});

const JrAlert = defineComponent({
    name: 'JrAlert',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const severity = s.severity || 'info';
            const icons = { info: 'ℹ️', warning: '⚠️', error: '❌', success: '✅' };
            return h('div', { class: `jr-root jr-alert jr-alert-${severity}` }, [
                h('span', { class: 'jr-alert-icon' }, icons[severity] || 'ℹ️'),
                h('div', { class: 'jr-alert-content' }, [
                    s.title ? h('strong', { class: 'jr-alert-title' }, s.title) : null,
                    h('span', { class: 'jr-alert-msg' }, s.message || ''),
                ]),
            ]);
        };
    },
});

const JrBadge = defineComponent({
    name: 'JrBadge',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const variant = s.variant || 'default';
            return h('span', { class: `jr-root jr-badge jr-badge-${variant}` }, s.text || '');
        };
    },
});

const JrCodeBlock = defineComponent({
    name: 'JrCodeBlock',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            return h('div', { class: 'jr-root', style: { position: 'relative' } }, [
                s.language ? h('span', { class: 'jr-codeblock-lang' }, s.language) : null,
                h('pre', { class: 'jr-codeblock' }, s.code || ''),
            ]);
        };
    },
});

const JrList = defineComponent({
    name: 'JrList',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const ordered = s.ordered === true;
            const tag = ordered ? 'ol' : 'ul';
            const items = (s.items || []).map((item, i) =>
                h('li', { key: i, class: 'jr-list-item' }, String(item))
            );
            return h(tag, { class: 'jr-root jr-list' }, items);
        };
    },
});

const JrProgress = defineComponent({
    name: 'JrProgress',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            const pct = Math.min(100, Math.max(0, Number(s.value) || 0));
            return h('div', { class: 'jr-root jr-progress' }, [
                h('div', { class: 'jr-progress-header' }, [
                    s.label ? h('span', { class: 'jr-progress-label' }, s.label) : null,
                    h('span', { class: 'jr-progress-pct' }, `${pct}%`),
                ]),
                h('div', { class: 'jr-progress-track' }, [
                    h('div', { class: 'jr-progress-fill', style: { width: `${pct}%` } }),
                ]),
            ]);
        };
    },
});

const JrSeparator = defineComponent({
    name: 'JrSeparator',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            if (s.label) {
                return h('div', { class: 'jr-root jr-separator-labeled' }, [
                    h('hr', { class: 'jr-separator-line' }),
                    h('span', { class: 'jr-separator-text' }, s.label),
                    h('hr', { class: 'jr-separator-line' }),
                ]);
            }
            return h('hr', { class: 'jr-root jr-separator' });
        };
    },
});

const JrText = defineComponent({
    name: 'JrText',
    props: { spec: Object },
    setup(props) {
        return () => {
            const s = props.spec || {};
            return h('p', { class: 'jr-root', style: { margin: 0, fontSize: '12px', lineHeight: '1.5' } }, s.text || '');
        };
    },
});

// ──── Chart (ECharts) ────

const JrChart = defineComponent({
    name: 'JrChart',
    props: { spec: Object },
    setup(props) {
        const containerRef = ref(null);
        let chartInstance = null;

        function initChart() {
            if (!containerRef.value || typeof echarts === 'undefined') return;
            if (chartInstance) { chartInstance.dispose(); chartInstance = null; }
            chartInstance = echarts.init(containerRef.value, 'dark', { renderer: 'canvas' });
            const option = props.spec?.option || props.spec || {};
            chartInstance.setOption(option);
        }

        onMounted(() => { nextTick(initChart); });

        watch(() => props.spec, () => {
            if (chartInstance) {
                const option = props.spec?.option || props.spec || {};
                chartInstance.setOption(option, true);
            } else {
                nextTick(initChart);
            }
        }, { deep: true });

        onBeforeUnmount(() => {
            if (chartInstance) { chartInstance.dispose(); chartInstance = null; }
        });

        return () => h('div', {
            ref: containerRef,
            class: 'jr-root jr-chart',
            style: { width: '100%', height: `${props.spec?.height || 300}px` },
        });
    },
});

// ──── Widget 注册表 ────

export const WIDGET_REGISTRY = {
    Card: { component: JrCard },
    Metric: { component: JrMetric },
    Stack: { component: JrStack },
    Heading: { component: JrHeading },
    Table: { component: JrTable },
    Alert: { component: JrAlert },
    Badge: { component: JrBadge },
    CodeBlock: { component: JrCodeBlock },
    List: { component: JrList },
    Progress: { component: JrProgress },
    Separator: { component: JrSeparator },
    Text: { component: JrText },
    Chart: { component: JrChart },
};
