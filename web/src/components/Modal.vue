<script setup>
defineProps({
  title: { type: String, default: '' },
  wide: { type: Boolean, default: false }
})
defineEmits(['close'])
</script>

<template>
  <div id="modal-root" @click.self="$emit('close')">
    <div class="modal" :class="{ wide }">
      <h3>{{ title }}</h3>
      <slot />
      <div class="actions">
        <slot name="actions" />
      </div>
    </div>
  </div>
</template>
