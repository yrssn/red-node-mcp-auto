<template>
  <el-drawer
    :visible="visible"
    @update:visible="$emit('update:visible', $event)"
    :title="comment ? '评论详情' : '帖子详情'"
    size="580px"
  >
    <div v-if="post" class="detail-content">
      <div class="eyebrow">原帖内容</div>
      <h2>{{ post.title || "图片帖（无文字标题）" }}</h2>
      <div class="detail-meta">{{ post.author }} · {{ taskName }}</div>
      <div class="post-decision">
        <el-tag size="small" :type="decisionType(post)">{{
          decisionLabel(post)
        }}</el-tag>
        <span v-if="post.images && post.images.length" class="muted"
          >{{ post.images.length }} 张图片</span
        >
      </div>
      <p class="post-body">
        {{ post.content || "该帖没有文字正文，请结合图片 OCR 或原帖查看。" }}
      </p>
      <template v-if="post.author_updates">
        <div class="eyebrow">原作者补充（已加载评论）</div>
        <p class="post-body">{{ post.author_updates }}</p>
      </template>
      <a
        v-if="safeURL"
        :href="post.url"
        target="_blank"
        rel="noopener noreferrer"
        class="external-link"
        >打开原帖 <i class="el-icon-top-right"></i
      ></a>
      <div v-else-if="demo" class="field-hint">示例帖子，无真实站点链接</div>
      <el-divider></el-divider>
      <div class="panel-heading">
        <h3>图片文字识别（本地 OCR）</h3>
        <el-tag
          size="small"
          :type="
            post.ocr_status === 'failed'
              ? 'danger'
              : ['done', 'success'].includes(post.ocr_status)
              ? 'success'
              : post.ocr_status === 'partial'
              ? 'warning'
              : 'info'
          "
          >{{ ocrStatusLabel(post.ocr_status) }}</el-tag
        >
      </div>
      <pre v-if="post.ocr_text" class="ocr-text">{{ post.ocr_text }}</pre>
      <p v-else class="muted">
        {{
          ["done", "success", "empty"].includes(post.ocr_status)
            ? "未识别到图片文字。"
            : "暂无图片文字识别结果。"
        }}
      </p>
      <div v-if="post.ocr_error" class="inline-error">{{ post.ocr_error }}</div>
      <div v-if="post.decision_status === 'review'" class="info-strip">
        待人工确认：请结合原帖、图片文字和判断理由确认需求。
      </div>
      <el-divider></el-divider>
      <div class="panel-heading">
        <h3>筛选结果</h3>
        <span class="muted"
          >来源：{{ analysisMethodLabel(post.analysis_method) }}</span
        >
      </div>
      <p class="analysis-reason">{{ post.reason || "等待分析" }}</p>
      <el-button
        size="small"
        :loading="reanalyzing"
        @click="$emit('reanalyze', post)"
        >重新分析</el-button
      >
      <div class="field-hint">
        仅刷新该帖详情、OCR 和判断，不找新帖、不生成或发送评论。
      </div>
      <template v-if="!comment">
        <el-divider></el-divider>
        <h3>人工评论草稿</h3>
        <template v-if="relatedComment">
          <p class="muted">
            该帖已有评论记录，可继续查看或编辑，避免重复创建。
          </p>
          <el-button size="small" @click="$emit('view-comment', relatedComment)"
            >查看已有评论</el-button
          >
        </template>
        <template v-else>
          <emoji-textarea
            v-model="draftText"
            :rows="5"
            :disabled="busy"
            :maxlength="draftMaxLength"
            placeholder="写下与这条帖子相关的交流内容"
            show-word-limit
          ></emoji-textarea>
          <div class="field-hint">
            使用当前任务策略的字数上限。保存仅创建待审核草稿，不调用
            AI、不发送评论。
          </div>
          <div class="detail-actions">
            <el-button
              type="primary"
              size="small"
              :loading="busy"
              :disabled="!draftValid"
              @click="$emit('save-draft', draftText)"
              >保存人工草稿</el-button
            >
          </div>
        </template>
      </template>
      <template v-if="comment">
        <el-divider></el-divider>
        <div class="panel-heading">
          <h3>评论内容</h3>
          <el-tag size="small">{{ labels[comment.status] }}</el-tag>
        </div>
        <div class="field-hint">
          {{ comment.strategy.name }} · v{{ comment.strategy.version }} · 账号
          {{ comment.account_id }}
        </div>
        <div v-if="comment.status === 'queued'" class="queue-wait-notice">
          <strong>等待原因</strong>
          <p>{{ queueReason }}</p>
          <el-button
            type="text"
            size="small"
            @click="$emit('view-task', comment.task_id)"
            >查看任务发送开关</el-button
          >
        </div>
        <emoji-textarea
          v-model="text"
          :rows="5"
          :disabled="!editable"
          :maxlength="comment.strategy.max_length"
          show-word-limit
        ></emoji-textarea>
        <div v-if="comment.error" class="inline-error">{{ comment.error }}</div>
        <div class="detail-actions">
          <el-button
            v-if="editable"
            size="small"
            :loading="busy"
            @click="$emit('action', 'edit', text)"
            >保存修改</el-button
          >
          <el-button
            v-if="regeneratable"
            size="small"
            :loading="busy"
            @click="regenerate"
            >重新生成</el-button
          >
          <el-button
            v-if="reviewable"
            size="small"
            type="primary"
            :loading="busy"
            @click="$emit('action', 'approve', text)"
            >通过并入队</el-button
          >
          <el-button
            v-if="reviewable || comment.status === 'queued'"
            size="small"
            @click="$emit('action', 'skip')"
            >跳过</el-button
          >
          <el-button
            v-if="comment.status === 'queued'"
            size="small"
            @click="$emit('action', 'cancel')"
            >取消排队</el-button
          >
          <template v-if="comment.status === 'uncertain'"
            ><el-button
              size="small"
              type="primary"
              @click="resolve('confirm_sent')"
              >核实已发送</el-button
            ><el-button size="small" @click="resolve('confirm_failed')"
              >核实未发送</el-button
            ></template
          >
        </div>
        <el-collapse
          ><el-collapse-item title="AI 原稿与实际发送内容"
            ><div class="field-hint">AI 原稿</div>
            <p class="post-body">{{ comment.original }}</p>
            <div class="field-hint">实际发送</div>
            <p class="post-body">
              {{ comment.sent_text || "尚未确认发送" }}
            </p></el-collapse-item
          ><el-collapse-item
            v-if="comment.strategy_versions && comment.strategy_versions.length"
            title="采用的策略版本"
            ><div v-for="(strategy, i) in comment.strategy_versions" :key="i">
              <strong>{{ strategy.name }} · v{{ strategy.version }}</strong>
              <p class="post-body">
                {{ strategy.prompt || strategy.fixed_text }}
              </p>
            </div></el-collapse-item
          ></el-collapse
        >
        <h3 class="history-heading">操作记录</h3>
        <el-timeline
          ><el-timeline-item
            v-for="(h, i) in history"
            :key="i"
            :timestamp="date(h.at)"
            placement="top"
            ><strong>{{ historyLabel(h.action) }}</strong>
            <p class="history-text">{{ h.text }}</p></el-timeline-item
          ></el-timeline
        >
      </template>
    </div>
  </el-drawer>
</template>
<script>
import EmojiTextarea from "./EmojiTextarea.vue";
import {
  decisionLabel,
  decisionType,
  ocrStatusLabel,
  analysisMethodLabel,
} from "./postAnalysis";

export default {
  components: { EmojiTextarea },
  props: [
    "visible",
    "post",
    "comment",
    "taskName",
    "demo",
    "busy",
    "reanalyzing",
    "relatedComment",
    "draftMaxLength",
    "queueReason",
  ],
  data: () => ({
    text: "",
    draftText: "",
    labels: {
      draft: "草稿",
      review: "待审核",
      queued: "待发送",
      sending: "发送中",
      sent: "已发送",
      failed: "失败",
      uncertain: "待确认",
      skipped: "已跳过",
      cancelled: "已取消",
    },
  }),
  computed: {
    draftValid() {
      return (
        this.draftText.trim() !== "" &&
        Array.from(this.draftText).length <=
          (Number(this.draftMaxLength) || 500)
      );
    },
    editable() {
      return (
        this.comment &&
        [
          "draft",
          "review",
          "queued",
          "failed",
          "cancelled",
          "skipped",
        ].includes(this.comment.status)
      );
    },
    regeneratable() {
      return (
        this.comment &&
        ["draft", "review", "failed", "cancelled", "skipped"].includes(
          this.comment.status
        )
      );
    },
    reviewable() {
      return (
        this.comment &&
        ["draft", "review", "failed"].includes(this.comment.status)
      );
    },
    history() {
      return [...(this.comment?.history || [])].reverse();
    },
    safeURL() {
      try {
        const u = new URL(this.post?.url);
        return (
          u.protocol === "https:" &&
          ["www.xiaohongshu.com", "xiaohongshu.com"].includes(u.hostname)
        );
      } catch {
        return false;
      }
    },
  },
  watch: {
    visible(value) {
      if (value) {
        this.text = this.comment?.text || "";
        this.draftText = "";
      }
    },
    "post.id"() {
      this.draftText = "";
    },
    "comment.text"(text) {
      this.text = text || "";
    },
  },
  methods: {
    decisionLabel,
    decisionType,
    ocrStatusLabel,
    analysisMethodLabel,
    date(value) {
      return value
        ? new Date(value * 1000).toLocaleString("zh-CN", { hour12: false })
        : "—";
    },
    historyLabel(action) {
      return (
        {
          edit: "修改内容",
          approve: "审核通过",
          skip: "跳过",
          cancel: "取消排队",
          confirm_sent: "人工核实已发送",
          confirm_failed: "人工核实未发送",
          sent: "发送成功",
          uncertain: "发送结果待确认",
        }[action] || action
      );
    },
    async regenerate() {
      try {
        const { value } = await this.$prompt(
          "填写修改要求，例如：更简短、结合帖子中的预算问题。",
          "重新生成",
          {
            inputPlaceholder: "留空使用最新策略重新生成",
            confirmButtonText: "生成",
          }
        );
        this.$emit("action", "regenerate", value || "");
      } catch {}
    },
    async resolve(action) {
      try {
        await this.$confirm(
          action === "confirm_sent"
            ? "已在原帖确认该评论存在？"
            : "已在原帖确认该评论未发送？后续可重新审核。",
          "核实发送结果"
        );
        this.$emit("action", action);
      } catch {}
    },
  },
};
</script>
