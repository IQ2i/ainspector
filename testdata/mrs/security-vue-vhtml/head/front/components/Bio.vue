<script setup lang="ts">
defineProps<{ bio: string }>()
</script>

<template>
  <section class="bio">
    <h2>Biography</h2>
    <div v-html="bio"></div>
  </section>
</template>
