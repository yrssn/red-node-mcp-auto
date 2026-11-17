<template>
  <el-dialog
    :title="(form.id ? '编辑' : '新建') + names[kind]"
    :visible="visible"
    @update:visible="$emit('update:visible', $event)"
    width="680px"
    :close-on-click-modal="false"
  >
    <el-form v-if="visible" label-position="top" :model="form">
      <el-form-item label="名称" required
        ><el-input
          v-model="form.name"
          maxlength="80"
          placeholder="起一个容易识别的名称"
        ></el-input
      ></el-form-item>
      <template v-if="kind === 'models'">
        <el-form-item label="API 基础地址" required
          ><el-input
            v-model="form.base_url"
            placeholder="https://你的模型服务/v1"
          ></el-input
        ></el-form-item>
        <el-form-item label="模型标识" required
          ><el-input
            v-model="form.model"
            placeholder="模型服务提供的 model 名称"
          ></el-input
        ></el-form-item>
        <el-form-item label="API Key"
          ><el-input
            v-model="form.api_key"
            type="password"
            show-password
            :placeholder="
              form.has_key ? '已配置，留空保留原密钥' : '本地模型无密钥时可留空'
            "
            autocomplete="new-password"
          ></el-input
        ></el-form-item>
      </template>
      <template v-if="kind === 'strategies'">
        <div class="form-row">
          <el-form-item label="生成方式"
            ><el-radio-group v-model="form.mode"
              ><el-radio-button label="ai">AI 生成</el-radio-button
              ><el-radio-button label="fixed"
                >固定话术</el-radio-button
              ></el-radio-group
            ></el-form-item
          ><el-form-item label="启用"
            ><el-switch v-model="form.enabled"></el-switch
          ></el-form-item>
        </div>
        <el-form-item label="评论策略 / 语气要求"
          ><emoji-textarea
            v-model="form.prompt"
            :rows="4"
            placeholder="例如：针对帖子具体问题回复，语气友善，提供一条实用建议。"
          ></emoji-textarea
        ></el-form-item>
        <el-form-item v-if="form.mode === 'fixed'" label="固定评论正文" required
          ><emoji-textarea v-model="form.fixed_text" :rows="3"></emoji-textarea
        ></el-form-item>
        <el-form-item label="最大字数"
          ><el-input-number
            v-model="form.max_length"
            :min="1"
            :max="500"
          ></el-input-number
        ></el-form-item>
        <el-form-item label="禁用词"
          ><el-input
            v-model="form.forbidden"
            placeholder="多个词用逗号分隔"
          ></el-input
        ></el-form-item>
      </template>
      <template v-if="kind === 'materials'">
        <el-form-item label="分类"
          ><el-input
            v-model="form.category"
            placeholder="如：知识、参考回复、产品信息"
          ></el-input
        ></el-form-item>
        <el-form-item label="素材内容" required
          ><emoji-textarea v-model="form.content" :rows="7"></emoji-textarea
        ></el-form-item>
        <el-form-item label="图片参考附件">
          <input
            ref="materialImageInput"
            class="material-file-input"
            type="file"
            accept="image/png,image/jpeg"
            @change="selectMaterialImage"
          />
          <div v-if="imageSource" class="material-image-preview">
            <el-image
              :src="imageSource"
              :preview-src-list="[imageSource]"
              fit="contain"
              alt="素材参考图片"
            ></el-image>
          </div>
          <div class="material-image-actions">
            <el-button
              size="small"
              :loading="imageReading"
              :disabled="busy"
              @click="$refs.materialImageInput.click()"
              >{{ form.image ? "替换图片" : "选择本地图片" }}</el-button
            >
            <el-button
              v-if="form.image"
              size="small"
              :disabled="busy || imageReading"
              @click="$set(form, 'image', '')"
              >移除图片</el-button
            >
          </div>
          <div class="field-hint">
            静态 PNG / JPEG，最大 512 KiB，暂不支持
            GIF。选择后仅本地预览，保存后写入素材库，不改变素材启用状态。
          </div>
          <div class="field-hint">
            图片仅作参考，当前不支持图片/原生表情自动评论，也不会将附件发送给模型。
          </div>
        </el-form-item>
        <el-form-item label="参与 AI 生成"
          ><el-switch v-model="form.enabled"></el-switch
        ></el-form-item>
      </template>
      <template v-if="kind === 'tasks'">
        <el-form-item label="找帖来源（可组合）" required>
          <div v-for="(src, i) in form.sources" :key="i" class="source-editor">
            <el-select v-model="src.kind"
              ><el-option
                v-for="(label, key) in sources"
                :key="key"
                :value="key"
                :label="label"
              ></el-option></el-select
            ><el-input
              v-model="src.value"
              :placeholder="
                ['profile', 'link'].includes(src.kind)
                  ? '完整 HTTPS 链接，保留 xsec_token'
                  : '输入关键词或话题词'
              "
            ></el-input
            ><el-button
              icon="el-icon-minus"
              circle
              @click="form.sources.splice(i, 1)"
              :disabled="form.sources.length === 1"
            ></el-button>
          </div>
          <el-button
            type="text"
            icon="el-icon-plus"
            @click="form.sources.push({ kind: 'keyword', value: '' })"
            >添加来源</el-button
          >
          <div class="field-hint">
            话题按关键词搜索；主页与链接读取当前可访问内容。发布时间与类型筛选用于搜索来源。
          </div>
        </el-form-item>
        <el-form-item v-if="kind === 'tasks'" label="分析方式">
          <div class="field-hint">帖子图片由本地 OCR 提取文字，再由文本模型判断。</div>
        </el-form-item>
        <template v-if="kind === 'tasks'">
          <el-form-item label="主题关键词（逗号分隔）">
            <el-input
              v-model="form.match_keywords"
              placeholder="如：Java,JavaWeb,Spring Boot"
            ></el-input>
          </el-form-item>
          <el-form-item label="需求信号词（逗号分隔）">
            <el-input
              v-model="form.demand_keywords"
              placeholder="如：求助,急求,需要帮助"
            ></el-input>
          </el-form-item>
        </template>
        <el-form-item label="保留已结束需求">
          <el-switch v-model="form.include_closed"></el-switch>
          <div class="field-hint">
            已找到或已解决的帖子仍标记“需求已结束”，保留为待人工确认候选，仅人工处理。
          </div>
        </el-form-item>
        <div class="form-row">
          <el-form-item label="模型"
            ><el-select
              v-model="form.model_id"
              placeholder="可稍后配置"
              clearable
              ><el-option
                v-for="m in state.models"
                :key="m.id"
                :value="m.id"
                :label="m.name"
              ></el-option
            ></el-select>
            <div class="field-hint">
              可留空保存暂停任务，运行或恢复前须配置模型。
            </div></el-form-item
          ><el-form-item label="评论策略" required
            ><el-select v-model="form.strategy_id" placeholder="选择评论策略"
              ><el-option
                v-for="s in state.strategies"
                :key="s.id"
                :value="s.id"
                :label="s.name + ' · v' + s.version"
                :disabled="!s.enabled"
              ></el-option></el-select
          ></el-form-item>
        </div>
        <el-form-item label="模型补充分析目标"
          ><el-input
            v-model="form.goal"
            type="textarea"
            :rows="3"
            placeholder="描述希望模型找到的帖子及排除条件。"
          ></el-input
        ></el-form-item>
        <el-form-item label="排除词"
          ><el-input
            v-model="form.exclude"
            placeholder="多个词用逗号分隔，作为模型的排除条件"
          ></el-input
        ></el-form-item>
        <div class="form-row">
          <el-form-item label="发布时间"
            ><el-select v-model="form.publish_time"
              ><el-option
                v-for="v in ['不限', '一天内', '一周内', '半年内']"
                :key="v"
                :value="v"
              ></el-option></el-select></el-form-item
          ><el-form-item label="帖子类型"
            ><el-select v-model="form.note_type"
              ><el-option
                v-for="v in ['不限', '图文', '视频']"
                :key="v"
                :value="v"
              ></el-option></el-select
          ></el-form-item>
        </div>
        <div class="form-row">
          <el-form-item label="每轮获取 / 分析上限"
            ><el-input-number
              v-model="form.limit"
              :min="1"
              :max="50"
            ></el-input-number></el-form-item
          ><el-form-item label="每轮发送上限"
            ><el-input-number
              v-model="form.send_limit"
              :min="1"
              :max="50"
            ></el-input-number
          ></el-form-item>
        </div>
        <el-form-item label="评论处理方式"
          ><el-radio-group v-model="form.auto_send"
            ><el-radio :label="false">人工审核后发送</el-radio
            ><el-radio :label="true"
              >匹配后自动入队（受策略约束）</el-radio
            ></el-radio-group
          >
          <div class="field-hint">
            仅模型完成判断的结果可按配置自动入队。
          </div>
          <div class="field-hint">
            发送达到每轮上限后，队列等待下一轮任务运行。实际发送受全局队列开关控制。
          </div></el-form-item
        >
        <div class="form-row">
          <el-form-item label="定时刷新"
            ><el-switch v-model="form.scheduled"></el-switch></el-form-item
          ><el-form-item label="刷新间隔（秒）"
            ><el-input-number
              v-model="form.interval"
              :min="60"
              :max="86400"
            ></el-input-number
          ></el-form-item>
        </div>
      </template>
    </el-form>
    <span slot="footer"
      ><el-button @click="$emit('update:visible', false)">取消</el-button
      ><el-button
        type="primary"
        :loading="busy"
        :disabled="imageReading"
        @click="$emit('save', form)"
        >保存</el-button
      ></span
    >
  </el-dialog>
</template>
<script>
import EmojiTextarea from "./EmojiTextarea.vue";
import { referenceImage } from "./referenceImage";

export default {
  components: { EmojiTextarea },
  props: ["visible", "kind", "initial", "state", "busy"],
  data: () => ({
    form: {},
    imageReading: false,
    names: {
      tasks: "任务",
      models: "模型",
      strategies: "策略",
      materials: "素材",
    },
    sources: {
      keyword: "关键词",
      profile: "用户主页",
      tag: "话题词",
      link: "帖子链接",
    },
  }),
  computed: {
    imageSource() {
      return referenceImage(this.form.image);
    },
  },
  watch: {
    visible(value) {
      if (this.imageReader) this.imageReader.abort();
      this.imageReading = false;
      if (value) {
        const form = JSON.parse(JSON.stringify(this.initial));
        if (this.kind === "materials" && typeof form.image !== "string") {
          form.image = "";
        }
        if (this.kind === "tasks") {
          form.analysis_mode = "model";
          form.match_keywords = form.match_keywords || "";
          form.demand_keywords = form.demand_keywords || "";
          form.include_closed = !!form.include_closed;
        }
        this.form = form;
      }
    },
  },
  beforeDestroy() {
    if (this.imageReader) this.imageReader.abort();
  },
  methods: {
    selectMaterialImage(event) {
      const file = event.target.files?.[0];
      event.target.value = "";
      if (!file) return;
      if (!["image/png", "image/jpeg"].includes(file.type)) {
        this.$message.warning("请选择静态 PNG 或 JPEG 图片，暂不支持 GIF。");
        return;
      }
      if (file.size > 512 * 1024) {
        this.$message.warning("图片不能超过 512 KiB，请选择原图或更小的文件。");
        return;
      }
      if (this.imageReader) this.imageReader.abort();
      const form = this.form;
      const reader = new FileReader();
      this.imageReader = reader;
      this.imageReading = true;
      reader.onload = () => {
        if (this.visible && this.form === form) {
          this.$set(form, "image", reader.result);
        }
      };
      reader.onerror = () => {
        if (this.visible && this.form === form) {
          this.$message.error("读取图片失败，请重新选择。");
        }
      };
      reader.onloadend = () => {
        if (this.imageReader === reader) {
          this.imageReading = false;
          this.imageReader = null;
        }
      };
      reader.readAsDataURL(file);
    },
  },
};
</script>
