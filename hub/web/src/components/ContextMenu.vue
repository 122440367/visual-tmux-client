<script setup lang="ts">
// A right-click menu built from a list of items, for the desktop shell's
// session rows and terminal. A native window is expected to answer a right
// click, and the web view's own menu (Reload, Back, Inspect) is not about
// anything in this application. It follows the file manager's menu
// (files/FileContextMenu.vue) in how it is reached and left: it takes the focus
// when it opens, answers the arrow keys, Home, End, Escape and Tab, and closes
// on a press elsewhere, a scroll or a resize.
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

export interface MenuItem {
  id: string
  label: string
  /** Accelerator label, shown right-aligned the way a native menu shows one. */
  shortcut?: string
  disabled?: boolean
  danger?: boolean
  /** Draws a divider above this item. */
  separated?: boolean
}

const props = defineProps<{ x: number; y: number; items: MenuItem[]; title?: string }>()
const emit = defineEmits<{
  (e: 'select', id: string): void
  (e: 'close'): void
}>()

const menu = ref<HTMLElement | null>(null)
const offset = ref({ left: props.x, top: props.y })

function place() {
  const el = menu.value
  if (!el) return
  const { width, height } = el.getBoundingClientRect()
  offset.value = {
    left: Math.max(4, Math.min(props.x, window.innerWidth - width - 4)),
    top: Math.max(4, Math.min(props.y, window.innerHeight - height - 4)),
  }
}

function buttons(): HTMLButtonElement[] {
  const el = menu.value
  if (!el) return []
  return Array.from(el.querySelectorAll<HTMLButtonElement>('[role="menuitem"]:not(:disabled)'))
}

function focusItem(index: number) {
  const found = buttons()
  if (found.length === 0) return
  found[((index % found.length) + found.length) % found.length].focus()
}

function choose(item: MenuItem) {
  if (item.disabled) return
  emit('select', item.id)
  emit('close')
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    event.preventDefault()
    emit('close')
    return
  }
  const found = buttons()
  const current = found.indexOf(document.activeElement as HTMLButtonElement)
  if (current < 0) return
  const move: Record<string, number> = {
    ArrowDown: current + 1,
    ArrowUp: current - 1,
    Home: 0,
    End: found.length - 1,
  }
  const to = move[event.key]
  if (to === undefined) {
    if (event.key === 'Tab') emit('close')
    return
  }
  event.preventDefault()
  focusItem(to)
}

function onPointerDown(event: PointerEvent) {
  if (menu.value && !menu.value.contains(event.target as Node)) emit('close')
}

function onViewportChange() {
  emit('close')
}

onMounted(() => {
  place()
  focusItem(0)
  document.addEventListener('keydown', onKeydown, true)
  document.addEventListener('pointerdown', onPointerDown, true)
  window.addEventListener('scroll', onViewportChange, { capture: true, passive: true })
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('blur', onViewportChange)
})

// A second right-click reuses this instance at a new point; see the file
// manager's menu for why the placement has to follow it.
watch(
  () => [props.x, props.y, props.items],
  async () => {
    await nextTick()
    place()
    focusItem(0)
  },
  { flush: 'post' },
)

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKeydown, true)
  document.removeEventListener('pointerdown', onPointerDown, true)
  window.removeEventListener('scroll', onViewportChange, { capture: true })
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('blur', onViewportChange)
})
</script>

<template>
  <div
    ref="menu"
    class="ctx"
    :style="{ left: `${offset.left}px`, top: `${offset.top}px` }"
    role="menu"
    :aria-label="title"
    @contextmenu.prevent
  >
    <div v-if="title" class="ctx__title" :title="title">{{ title }}</div>
    <template v-for="item in items" :key="item.id">
      <div v-if="item.separated" class="ctx__sep" role="separator"></div>
      <button
        class="ctx__item"
        :class="{ 'ctx__item--danger': item.danger }"
        type="button"
        role="menuitem"
        :disabled="item.disabled"
        @click="choose(item)"
      >
        <span class="ctx__label">{{ item.label }}</span>
        <span v-if="item.shortcut" class="ctx__shortcut">{{ item.shortcut }}</span>
      </button>
    </template>
  </div>
</template>

<style scoped>
.ctx {
  position: fixed;
  z-index: 900;
  min-width: 210px;
  max-width: 320px;
  padding: 4px;
  background: var(--th-raised);
  border: 1px solid var(--th-border-strong, var(--th-border));
  border-radius: 8px;
  box-shadow:
    0 12px 32px rgb(0 0 0 / 50%),
    0 1px 3px rgb(0 0 0 / 30%);
  font-family: var(--app-ui-font);
}
.ctx__title {
  padding: 5px 10px 6px;
  overflow: hidden;
  color: var(--th-text-lo);
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ctx__sep {
  height: 1px;
  margin: 4px 6px;
  background: var(--th-border-strong, var(--th-border));
}
.ctx__item {
  display: flex;
  align-items: center;
  gap: 1.5rem;
  width: 100%;
  padding: 5px 10px;
  color: var(--th-text-hi);
  font-size: 12.5px;
  text-align: left;
  background: none;
  border: none;
  border-radius: 5px;
  cursor: default;
}
.ctx__item:hover:not(:disabled),
.ctx__item:focus-visible {
  background: var(--th-accent);
  color: #0a0a0a;
  outline: none;
}
.ctx__item:hover:not(:disabled) .ctx__shortcut,
.ctx__item:focus-visible .ctx__shortcut {
  color: rgb(10 10 10 / 70%);
}
.ctx__item:disabled {
  color: var(--th-text-lo);
}
.ctx__item--danger {
  color: var(--th-danger);
}
.ctx__item--danger:hover:not(:disabled),
.ctx__item--danger:focus-visible {
  background: var(--th-danger);
  color: #0a0a0a;
}
.ctx__label {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ctx__shortcut {
  color: var(--th-text-lo);
  font-size: 11.5px;
  white-space: nowrap;
}
</style>
