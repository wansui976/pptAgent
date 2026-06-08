<template>
  <template v-if="slides.length">
    <Screen v-if="screening" />
    <Editor v-else-if="_isPC" />
    <Mobile v-else />
  </template>
  <FullscreenSpin tip="数据初始化中，请稍等 ..." v-else  loading :mask="false" />
</template>

<script lang="ts" setup>
import { onMounted, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { nanoid } from 'nanoid'
import { useScreenStore, useMainStore, useSnapshotStore, useSlidesStore } from '@/store'
import { LOCALSTORAGE_KEY_DISCARDED_DB } from '@/configs/storage'
import { deleteDiscardedDB } from '@/utils/database'
import { isPC } from '@/utils/common'
import api from '@/services'

import Editor from './views/Editor/index.vue'
import Screen from './views/Screen/index.vue'
import Mobile from './views/Mobile/index.vue'
import FullscreenSpin from '@/components/FullscreenSpin.vue'

const _isPC = isPC()

const mainStore = useMainStore()
const slidesStore = useSlidesStore()
const snapshotStore = useSnapshotStore()
const screenStore = useScreenStore()
const { databaseId } = storeToRefs(mainStore)
const { slides } = storeToRefs(slidesStore)
const { screening } = storeToRefs(screenStore)

const isAudienceMode = new URLSearchParams(window.location.search).get('mode') === 'audience'
const lingxiProject = new URLSearchParams(window.location.search).get('project') || ''
let lingxiSyncTimer: number | null = null
let lingxiSaveTimer: number | null = null
let lingxiHydrating = false
let lingxiLastLocalEditAt = 0
let lingxiInitialLoadDone = false

if (import.meta.env.MODE !== 'development') {
  window.onbeforeunload = () => false
}

const applyProjectPayload = async (payload: any) => {
  const project = payload?.data || payload
  if (!project) return

  lingxiHydrating = true
  slidesStore.setTitle(project.title || lingxiProject || '未命名演示文稿')

  if (project.width && Number(project.width) > 0) slidesStore.setViewportSize(Number(project.width))
  if (project.width && project.height && Number(project.width) > 0 && Number(project.height) > 0) {
    slidesStore.setViewportRatio(Number(project.height) / Number(project.width))
  }

  const normalizedSlides = Array.isArray(project.slides) && project.slides.length
    ? project.slides
    : [{ id: nanoid(10), elements: [] }]
  slidesStore.setSlides(normalizedSlides, project.theme || undefined)

  if (slidesStore.slideIndex > normalizedSlides.length - 1) {
    slidesStore.updateSlideIndex(Math.max(normalizedSlides.length - 1, 0))
  }

  lingxiHydrating = false
}

const loadLingxiProject = async () => {
  if (!lingxiProject) return
  if (Date.now() - lingxiLastLocalEditAt < 2000) return
  try {
    const payload = await api.getLingxiProject(lingxiProject)
    await applyProjectPayload(payload)
  } catch (e) {
    console.error('[lingxi] failed to load project:', e)
  }
}

const saveLingxiProject = async () => {
  if (!lingxiProject || lingxiHydrating) return
  await api.saveLingxiProject(lingxiProject, {
    title: slidesStore.title,
    width: slidesStore.viewportSize,
    height: slidesStore.viewportSize * slidesStore.viewportRatio,
    theme: slidesStore.theme,
    slides: slidesStore.slides,
  })
}

onMounted(async () => {
  if (isAudienceMode) {
    slidesStore.setSlides([{
      id: nanoid(10),
      elements: [],
    }])
    screenStore.setScreening(true)
  }
  else if (lingxiProject) {
    await loadLingxiProject()
    lingxiInitialLoadDone = true
    // 若加载失败 slides 仍为空，给一个空幻灯片防止永久 loading
    if (!slidesStore.slides.length) {
      slidesStore.setSlides([{ id: nanoid(10), elements: [] }])
    }
    await deleteDiscardedDB()
    snapshotStore.initSnapshotDatabase()

    lingxiSyncTimer = window.setInterval(() => {
      loadLingxiProject().catch(() => {})
    }, 2500)
  }
  else {
    const slides = await api.getMockData('slides')
    slidesStore.setSlides(slides)

    await deleteDiscardedDB()
    snapshotStore.initSnapshotDatabase()
  }
})

watch(
  () => [slidesStore.title, slidesStore.viewportSize, slidesStore.viewportRatio, slidesStore.slides],
  () => {
    if (!lingxiProject || lingxiHydrating || !lingxiInitialLoadDone) return
    lingxiLastLocalEditAt = Date.now()
    if (lingxiSaveTimer) window.clearTimeout(lingxiSaveTimer)
    lingxiSaveTimer = window.setTimeout(() => {
      saveLingxiProject().catch(() => {})
    }, 800)
  },
  { deep: true },
)

// 应用注销时向 localStorage 中记录下本次 indexedDB 的数据库ID，用于之后清除数据库
window.addEventListener('beforeunload', () => {
  if (lingxiSyncTimer) window.clearInterval(lingxiSyncTimer)
  if (lingxiSaveTimer) window.clearTimeout(lingxiSaveTimer)
  const discardedDB = localStorage.getItem(LOCALSTORAGE_KEY_DISCARDED_DB)
  const discardedDBList: string[] = discardedDB ? JSON.parse(discardedDB) : []

  discardedDBList.push(databaseId.value)

  const newDiscardedDB = JSON.stringify(discardedDBList)
  localStorage.setItem(LOCALSTORAGE_KEY_DISCARDED_DB, newDiscardedDB)
})
</script>

<style lang="scss">
#app {
  height: 100%;
}
</style>
