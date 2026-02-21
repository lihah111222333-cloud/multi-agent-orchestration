/**
 * JsonRenderer — 递归渲染 json-render spec 的 Vue 组件。
 * 接收一个 spec 对象 (含 type 字段)，根据 WIDGET_REGISTRY 查找对应组件进行渲染。
 */
import { h, defineComponent } from '../../lib/vue.esm-browser.prod.js';
import { WIDGET_REGISTRY } from './JsonRenderWidgets.js';

export const JsonRenderer = defineComponent({
    name: 'JsonRenderer',
    props: {
        spec: { type: Object, required: true },
    },
    setup(props) {
        return () => {
            const spec = props.spec;
            if (!spec || typeof spec !== 'object') {
                return h('span', { class: 'jr-empty' }, '(empty)');
            }

            const typeName = (spec.type || '').toString().trim();
            const entry = WIDGET_REGISTRY[typeName];

            if (!entry) {
                return h('div', { class: 'jr-unknown' }, [
                    h('span', { class: 'jr-unknown-type' }, `Unknown: ${typeName || '(no type)'}`),
                ]);
            }

            return h(entry.component, { spec });
        };
    },
});
