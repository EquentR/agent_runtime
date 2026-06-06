import { config } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'

const elScrollbarStub = defineComponent({
  props: {
    viewClass: {
      type: [String, Array, Object],
      default: '',
    },
    wrapClass: {
      type: [String, Array, Object],
      default: '',
    },
  },
  emits: ['scroll'],
  setup(props, { emit, slots, expose }) {
    const wrapRef = ref<HTMLElement | null>(null)

    function setScrollTop(value: number) {
      if (wrapRef.value) {
        wrapRef.value.scrollTop = value
      }
    }

    expose({
      wrapRef,
      setScrollTop,
    })

    return () =>
      h('div', { class: 'el-scrollbar' }, [
        h('div', {
          class: ['el-scrollbar__wrap', props.wrapClass],
          ref: wrapRef,
          onScroll: () => {
            if (!wrapRef.value) {
              return
            }
            emit('scroll', {
              scrollTop: wrapRef.value.scrollTop,
              scrollLeft: wrapRef.value.scrollLeft,
            })
          },
        }, [
          h('div', { class: ['el-scrollbar__view', props.viewClass] }, slots.default?.()),
        ]),
      ])
  },
})

config.global.stubs = {
  ...(config.global.stubs ?? {}),
  'el-scrollbar': elScrollbarStub,
}
