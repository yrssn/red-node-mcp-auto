<template>
  <div class="emoji-textarea">
    <el-input
      ref="input"
      v-bind="$attrs"
      :value="value"
      :disabled="disabled"
      type="textarea"
      @input="$emit('input', $event)"
    ></el-input>
    <div class="emoji-toolbar" aria-label="插入文字表情">
      <span class="emoji-label">添加表情</span>
      <button
        v-for="emoji in emojis"
        :key="emoji.text"
        type="button"
        :disabled="disabled"
        :aria-label="'插入' + emoji.name + '表情'"
        :title="emoji.name"
        @mousedown.prevent
        @click="insertEmoji(emoji.text)"
      >
        {{ emoji.text }}
      </button>
      <span
        v-if="showWordLimit && limit"
        class="emoji-counter"
        :class="{ 'is-over-limit': overLimit }"
        aria-live="polite"
      >
        {{ characterCount }} / {{ limit }}
      </span>
    </div>
    <div v-if="overLimit" class="emoji-limit-error" role="status">
      当前 {{ characterCount }} 字，超过 {{ limit }} 字上限，请删减后保存。
    </div>
    <div class="emoji-hint">
      文字表情，暂不支持图片/原生表情包。建议搭配正文使用，插入不会自动发送。
    </div>
  </div>
</template>

<script>
export default {
  inheritAttrs: false,
  props: {
    value: { type: String, default: "" },
    disabled: { type: Boolean, default: false },
    // 不透传原生 UTF-16 长度限制；输入超限仅提示，避免干扰中文 IME。
    maxlength: { type: [Number, String], default: undefined },
    showWordLimit: { type: Boolean, default: false },
  },
  data: () => ({
    emojis: [
      { text: "🙂", name: "微笑" },
      { text: "😊", name: "开心" },
      { text: "👍", name: "赞" },
      { text: "💻", name: "电脑" },
      { text: "🤝", name: "握手" },
    ],
  }),
  computed: {
    limit() {
      const value = Number(this.maxlength);
      return Number.isFinite(value) && value > 0 ? value : 0;
    },
    characterCount() {
      return Array.from(this.value || "").length;
    },
    overLimit() {
      return this.limit > 0 && this.characterCount > this.limit;
    },
  },
  methods: {
    insertEmoji(emoji) {
      if (this.disabled) return;
      const textarea = this.$refs.input.$refs.textarea;
      if (!textarea) return;
      const value = this.value || "";
      const start = textarea.selectionStart ?? value.length;
      const end = textarea.selectionEnd ?? value.length;
      const next = value.slice(0, start) + emoji + value.slice(end);
      if (this.limit > 0 && Array.from(next).length > this.limit) {
        this.$message.warning("已达到输入字数上限，请先删减文字。");
        return;
      }
      this.$emit("input", next);
      this.$nextTick(() => {
        textarea.focus();
        const position = start + emoji.length;
        textarea.setSelectionRange(position, position);
      });
    },
  },
};
</script>

<style scoped>
.emoji-toolbar {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 9px;
}
.emoji-label {
  margin-right: 3px;
  color: #9aa5b5;
  font-size: 11px;
  line-height: 1.6;
}
.emoji-toolbar button {
  display: grid;
  place-items: center;
  width: 29px;
  height: 27px;
  padding: 0;
  border: 1px solid #edf0f5;
  border-radius: 6px;
  background: #fafbfc;
  font-size: 17px;
}
.emoji-toolbar button:hover:not(:disabled) {
  border-color: #edc9cc;
  background: #fff3f3;
}
.emoji-toolbar button:focus-visible {
  outline: 2px solid #e3a0a5;
  outline-offset: 2px;
}
.emoji-toolbar button:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}
.emoji-hint {
  margin-top: 7px;
  color: #a2abba;
  font-size: 11px;
  line-height: 1.7;
}
.emoji-counter {
  margin-left: auto;
  color: #a2abba;
  font-size: 11px;
  line-height: 1.7;
}
.emoji-counter.is-over-limit,
.emoji-limit-error {
  color: #cb7278;
}
.emoji-limit-error {
  margin-top: 7px;
  font-size: 11px;
  line-height: 1.7;
}
</style>
