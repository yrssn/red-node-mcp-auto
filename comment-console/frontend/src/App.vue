<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark">言</span>
        <div>评论工作台<small>COMMENT STUDIO</small></div>
      </div>
      <div class="workspace-label">工作空间 <span>个人版</span></div>
      <nav>
        <button
          v-for="item in menus"
          :key="item.key"
          :class="{ active: page === item.key }"
          @click="navigate(item.key)"
        >
          <i :class="item.icon"></i>{{ item.label
          }}<em v-if="item.key === 'comments' && count('review')">{{
            count("review")
          }}</em>
        </button>
      </nav>
      <div class="sidebar-bottom">
        <span class="status-dot"></span>本地工作空间<small
          >v{{ state.version || "1.0.0" }} · Gin + Vue</small
        >
      </div>
    </aside>
    <div class="main-shell">
      <header class="topbar">
        <div>
          <span class="breadcrumb">工作空间</span
          ><i class="el-icon-arrow-right"></i>{{ currentMenu.label }}
        </div>
        <div class="top-actions">
          <span class="mode-tag" :class="{ demo: state.demo }">{{
            state.demo ? "演示模式 · 模拟发送" : "正式模式"
          }}</span
          ><el-button
            size="small"
            icon="el-icon-key"
            @click="tokenDialog = true"
            >访问凭证</el-button
          ><el-button
            size="small"
            :loading="accountBusy || accountCacheBusy"
            @click="openAccount"
            ><span
              class="status-dot"
              :class="{
                offline: accountPhase === 'offline',
                pending: ['unknown', 'checking'].includes(accountPhase),
                uncertain: accountPhase === 'uncertain',
              }"
            ></span
            >{{ accountText }}</el-button
          >
        </div>
      </header>
      <main>
        <div v-if="connectionError" class="connection-error">
          <i class="el-icon-warning-outline"></i> {{ connectionError }}
          <el-button type="text" @click="load()">重新连接</el-button
          ><el-button type="text" @click="tokenDialog = true"
            >填写访问凭证</el-button
          >
        </div>
        <div class="page-heading">
          <div>
            <div class="eyebrow">{{ currentMenu.en }}</div>
            <h1>{{ currentMenu.label }}</h1>
            <p>{{ descriptions[page] }}</p>
          </div>
          <div class="heading-actions">
            <span class="refresh-time" v-if="lastRefresh"
              >更新于 {{ lastRefresh }}</span
            ><el-button
              icon="el-icon-refresh"
              size="small"
              :loading="loading"
              @click="load()"
              >刷新</el-button
            ><el-button
              v-if="
                ['tasks', 'strategies', 'materials', 'models'].includes(page)
              "
              type="primary"
              icon="el-icon-plus"
              @click="openForm(page)"
              >新建{{ resourceNames[page] }}</el-button
            >
          </div>
        </div>
        <template v-if="page === 'overview'">
          <section class="welcome">
            <div>
              <span class="welcome-kicker">让每一次交流，都有上下文</span>
              <h2>发现合适的帖子，写下有帮助的评论。</h2>
              <p>
                从内容发现、图片 OCR
                到模型分析，再到审核与发送，所有进度都在这里。
              </p>
              <el-button type="primary" @click="openForm('tasks')"
                >创建任务 <i class="el-icon-right"></i></el-button
              ><el-button @click="navigate('comments')"
                >处理待审核评论</el-button
              >
            </div>
            <div class="welcome-art">
              <div class="art-orbit"></div>
              <i class="el-icon-chat-dot-square"></i
              ><span>发现 → 分析 → 交流</span>
            </div>
          </section>
          <div class="stat-grid">
            <div v-for="stat in stats" :key="stat.label" class="stat-card">
              <div class="stat-icon" :class="stat.color">
                <i :class="stat.icon"></i>
              </div>
              <div>
                <span>{{ stat.label }}</span
                ><strong>{{ stat.value }}</strong
                ><small>{{ stat.hint }}</small>
              </div>
            </div>
          </div>
          <div class="overview-grid">
            <section class="panel">
              <div class="panel-heading">
                <h3>
                  任务运行概况 <small>全部 {{ state.tasks.length }} 个</small>
                </h3>
                <el-button type="text" @click="navigate('tasks')"
                  >管理任务 <i class="el-icon-right"></i
                ></el-button>
              </div>
              <el-empty
                v-if="!state.tasks.length"
                description="还没有任务，从创建一个任务开始"
                :image-size="80"
              ></el-empty>
              <div class="overview-task" v-for="t in state.tasks" :key="t.id">
                <div class="task-symbol"><i class="el-icon-search"></i></div>
                <div class="grow">
                  <div class="overview-task-title">
                    <strong>{{ t.name }}</strong>
                    <el-tag size="mini" :type="taskStatusType(t)">{{
                      taskStatus(t)
                    }}</el-tag>
                  </div>
                  <small :title="sourceSummary(t)">{{
                    sourceSummary(t)
                  }}</small>
                  <p class="overview-task-progress">
                    <i v-if="t.running" class="el-icon-loading"></i>
                    {{ taskPhaseLabel(t) }} · {{ taskProgress(t) }}
                  </p>
                  <span class="overview-task-results">
                    累计 {{ taskResultCounts[t.id].posts }} 帖 · 已分析
                    {{ taskResultCounts[t.id].analyzed }} 帖
                  </span>
                </div>
                <div class="overview-task-actions">
                  <el-button type="text" @click="openTaskResults(t.id, 'posts')"
                    >查看帖子</el-button
                  >
                  <el-button
                    type="text"
                    :loading="taskSubmitting[t.id] === 'run'"
                    :disabled="t.running || t.queued || !!taskSubmitting[t.id]"
                    @click="taskAction(t, 'run')"
                    >{{ taskRunLabel(t) }}</el-button
                  >
                </div>
              </div>
            </section>
            <section class="panel">
              <div class="panel-heading">
                <h3>开始使用</h3>
                <span class="muted">4 个步骤</span>
              </div>
              <div
                class="checklist"
                v-for="(step, i) in steps"
                :key="step.title"
                @click="stepAction(i)"
              >
                <span class="step-num" :class="{ complete: step.done }">{{
                  step.done ? "✓" : i + 1
                }}</span>
                <div>
                  <strong>{{ step.title }}</strong
                  ><small>{{ step.detail }}</small>
                </div>
                <i class="el-icon-arrow-right"></i>
              </div>
            </section>
          </div>
          <section class="panel">
            <div class="panel-heading">
              <h3>最近活动</h3>
              <el-button type="text" @click="navigate('logs')"
                >查看日志</el-button
              >
            </div>
            <div class="activity" v-for="(l, i) in recentLogs" :key="i">
              <span class="activity-dot" :class="l.level"></span
              ><span>{{ l.message }}</span
              ><time>{{ date(l.at) }}</time>
            </div>
            <el-empty
              v-if="!recentLogs.length"
              description="任务运行后，这里会显示活动记录"
              :image-size="60"
            ></el-empty>
          </section>
        </template>
        <template v-if="page === 'tasks'">
          <div class="task-summary-strip">
            <div>
              <strong>全部 {{ state.tasks.length }} 个任务</strong>
              <span
                ><i class="task-state-dot running"></i>运行中
                {{ runningTaskCount }}</span
              >
              <span
                ><i class="task-state-dot queued"></i>排队中
                {{ queuedTaskCount }}</span
              >
            </div>
            <p>
              最多同时处理 2 个任务，进度与结果自动刷新；浏览器访问依次执行。
            </p>
          </div>
          <div class="filter-bar">
            <el-input
              v-model="taskSearch"
              placeholder="搜索任务名称或找帖关键词"
              prefix-icon="el-icon-search"
              clearable
            ></el-input
            ><span class="muted"
              >显示 {{ filteredTasks.length }} /
              {{ state.tasks.length }} 个任务</span
            >
            <el-button v-if="taskSearch" type="text" @click="taskSearch = ''"
              >清空搜索</el-button
            >
          </div>
          <div class="task-grid">
            <article
              class="panel task-card"
              v-for="t in filteredTasks"
              :key="t.id"
            >
              <div class="task-card-head">
                <div class="task-symbol">
                  <i class="el-icon-connection"></i>
                </div>
                <el-tag size="small" :type="taskStatusType(t)">{{
                  taskStatus(t)
                }}</el-tag
                ><el-dropdown
                  trigger="click"
                  @command="(command) => taskCommand(t, command)"
                  ><el-button type="text" icon="el-icon-more"></el-button
                  ><el-dropdown-menu slot="dropdown"
                    ><el-dropdown-item command="edit">编辑任务</el-dropdown-item
                    ><el-dropdown-item command="delete" divided
                      >删除任务</el-dropdown-item
                    ></el-dropdown-menu
                  ></el-dropdown
                >
              </div>
              <h3>{{ t.name }}</h3>
              <div
                class="task-progress"
                :class="{ active: t.running, queued: t.queued }"
                role="status"
              >
                <div class="task-progress-heading">
                  <i
                    :class="
                      t.running
                        ? 'el-icon-loading'
                        : t.queued
                        ? 'el-icon-time'
                        : 'el-icon-guide'
                    "
                  ></i>
                  <strong>{{ taskPhaseLabel(t) }}</strong>
                </div>
                <p>{{ taskProgress(t) }}</p>
              </div>
              <div class="task-result-grid">
                <button type="button" @click="openTaskResults(t.id, 'posts')">
                  <strong>{{ taskResultCounts[t.id].posts }}</strong
                  ><span>累计找到帖子</span>
                </button>
                <button type="button" @click="openTaskResults(t.id, 'posts')">
                  <strong>{{ taskResultCounts[t.id].analyzed }}</strong
                  ><span>已分析帖子</span>
                </button>
                <button
                  type="button"
                  @click="openTaskResults(t.id, 'comments')"
                >
                  <strong>{{ taskResultCounts[t.id].review }}</strong
                  ><span>待审核评论</span>
                </button>
              </div>
              <div class="task-result-links">
                <el-button
                  type="text"
                  icon="el-icon-document-copy"
                  @click="openTaskResults(t.id, 'posts')"
                  >查看帖子</el-button
                >
                <el-button
                  type="text"
                  icon="el-icon-tickets"
                  @click="openTaskResults(t.id, 'logs')"
                  >查看运行日志</el-button
                >
              </div>
              <p class="task-goal">{{ t.goal || "尚未填写分析目标" }}</p>
              <div class="source-tags">
                <el-tag size="mini" type="info">{{
                  analysisModeLabel(t.analysis_mode)
                }}</el-tag>
                <el-tag
                  v-if="!t.model_id"
                  size="mini"
                  type="warning"
                  >待配置模型</el-tag
                >
                <el-tag
                  v-for="(src, i) in t.sources"
                  :key="i"
                  size="mini"
                  type="info"
                  >{{ sourceLabels[src.kind] }} · {{ src.value }}</el-tag
                >
              </div>
              <div class="task-meta">
                <span
                  ><i class="el-icon-time"></i>
                  {{
                    t.scheduled ? "每 " + t.interval + " 秒" : "手动运行"
                  }}</span
                ><span>每轮 {{ t.limit }} 帖</span
                ><span>{{
                  t.auto_send
                    ? "模型判断后自动入队"
                    : "人工审核"
                }}</span>
              </div>
              <div class="task-meta">
                <span
                  >本轮已发送 {{ t.send_count || 0 }} / {{ t.send_limit }}</span
                ><span>上次运行 {{ date(t.last_run) }}</span>
              </div>
              <div class="stage-controls">
                <el-button
                  size="mini"
                  :disabled="!!taskSubmitting[t.id]"
                  :type="t.collect_paused ? 'warning' : ''"
                  @click="taskAction(t, 'collect')"
                  >{{ t.collect_paused ? "恢复找帖" : "暂停找帖" }}</el-button
                ><el-button
                  size="mini"
                  :disabled="!!taskSubmitting[t.id]"
                  :type="t.analyze_paused ? 'warning' : ''"
                  @click="taskAction(t, 'analyze')"
                  >{{ t.analyze_paused ? "恢复分析" : "暂停分析" }}</el-button
                ><el-button
                  size="mini"
                  :disabled="!!taskSubmitting[t.id]"
                  :type="t.send_paused ? 'warning' : ''"
                  @click="taskAction(t, 'send')"
                  >{{ t.send_paused ? "恢复发送" : "暂停发送" }}</el-button
                >
              </div>
              <div v-if="t.last_error" class="inline-error">
                {{ t.last_error }}
              </div>
              <div class="task-card-foot">
                <el-button
                  size="small"
                  :disabled="!!taskSubmitting[t.id]"
                  @click="taskAction(t, t.paused ? 'resume' : 'pause')"
                  >{{ t.paused ? "启用" : "暂停任务" }}</el-button
                ><el-button
                  size="small"
                  type="primary"
                  plain
                  :loading="taskSubmitting[t.id] === 'run'"
                  :disabled="t.running || t.queued || !!taskSubmitting[t.id]"
                  @click="taskAction(t, 'run')"
                  >{{ taskRunLabel(t) }}</el-button
                >
              </div>
            </article>
          </div>
          <el-empty
            v-if="!state.tasks.length"
            description="暂无任务，先配置评论策略再创建找帖任务，模型可稍后配置"
            ><el-button type="primary" @click="openForm('tasks')"
              >创建任务</el-button
            ></el-empty
          >
          <el-empty
            v-else-if="!filteredTasks.length"
            description="没有找到符合搜索条件的任务"
          >
            <el-button @click="taskSearch = ''"
              >清空搜索，查看全部任务</el-button
            >
          </el-empty>
        </template>
        <template v-if="page === 'posts'">
          <section class="panel table-panel">
            <div class="filter-bar">
              <el-input
                v-model="search"
                placeholder="搜索标题、正文、图片文字或作者"
                prefix-icon="el-icon-search"
                clearable
              ></el-input
              ><el-select v-model="taskFilter" clearable placeholder="全部任务"
                ><el-option
                  v-for="t in state.tasks"
                  :key="t.id"
                  :label="t.name"
                  :value="t.id"
                ></el-option></el-select
              ><el-select v-model="matchFilter" placeholder="全部分析结果"
                ><el-option label="全部分析结果" value=""></el-option
                ><el-option label="匹配" value="yes"></el-option
                ><el-option label="不匹配" value="no"></el-option
                ><el-option label="待人工确认" value="review"></el-option
                ><el-option
                  label="待分析"
                  value="pending"
                ></el-option></el-select
              ><span class="muted">{{ filteredPosts.length }} 条帖子</span>
            </div>
            <div v-if="selectedPosts.length || postBatchProgress" class="bulk-bar">
              {{ postBatchProgress || "已选择 " + selectedPosts.length + " 条" }}
              <el-button
                size="mini"
                :disabled="!!bulkBusy || !!reanalyzing"
                @click="reanalyzeSelected"
                >批量分析</el-button
              >
              <el-button
                size="mini"
                type="danger"
                plain
                :disabled="!!bulkBusy || !!reanalyzing"
                @click="deleteSelected('posts')"
                >批量删除</el-button
              >
            </div>
            <el-table
              ref="postsTable"
              :data="paged(filteredPosts)"
              row-key="id"
              @selection-change="selectedPosts = $event"
              @row-click="showPost"
              class="clickable"
              ><el-table-column type="selection" width="44" reserve-selection />
              <el-table-column label="帖子" min-width="250"
                ><template slot-scope="{ row }"
                  ><div class="post-title">
                    {{ row.title || "图片帖（无文字标题）" }}
                  </div>
                  <small
                    >{{ row.author || "作者待获取" }} ·
                    {{ taskName(row.task_id) }}</small
                  ></template
                ></el-table-column
              ><el-table-column
                label="发现渠道"
                min-width="150"
                prop="source"
                show-overflow-tooltip
              ></el-table-column
              ><el-table-column label="判断结果" width="125"
                ><template slot-scope="{ row }"
                  ><el-tag size="small" :type="decisionType(row)">{{
                    decisionLabel(row)
                  }}</el-tag></template
                ></el-table-column
              ><el-table-column label="内容分析 / OCR" min-width="155">
                <template slot-scope="{ row }">
                  <div>{{ analysisMethodLabel(row.analysis_method) }}</div>
                  <el-tooltip
                    v-if="row.ocr_error"
                    :content="row.ocr_error"
                    placement="top"
                  >
                    <small class="ocr-error-text"
                      >{{ ocrStatusLabel(row.ocr_status) }}
                      <i class="el-icon-warning-outline"></i
                    ></small>
                  </el-tooltip>
                  <small v-else>{{ ocrStatusLabel(row.ocr_status) }}</small>
                </template> </el-table-column
              ><el-table-column
                prop="reason"
                label="判断理由"
                min-width="210"
                show-overflow-tooltip
              ></el-table-column
              ><el-table-column label="发现时间" width="165"
                ><template slot-scope="{ row }">{{
                  date(row.created_at)
                }}</template></el-table-column
              ><el-table-column label="操作" width="108" fixed="right">
                <template slot-scope="{ row }">
                  <el-button
                    type="text"
                    :loading="reanalyzing === row.id"
                    :disabled="!!bulkBusy"
                    @click.stop="reanalyze(row)"
                    >重新分析</el-button
                  >
                </template>
              </el-table-column></el-table
            ><pagination
              :total="filteredPosts.length"
              :page.sync="pageNumber"
            ></pagination>
          </section>
        </template>
        <template v-if="page === 'comments'">
          <div
            class="queue-banner"
            :class="{
              paused: state.settings.send_paused || blockedQueueCount > 0,
            }"
          >
            <div>
              <i
                :class="
                  state.settings.send_paused || blockedQueueCount > 0
                    ? 'el-icon-video-pause'
                    : 'el-icon-video-play'
                "
              ></i
              ><strong>{{
                state.settings.send_paused ? "队列已暂停" : "队列已启用"
              }}</strong
              ><span
                >间隔 {{ state.settings.send_interval }} 秒 ·
                {{ count("queued") }} 条排队 · 有
                {{ blockedQueueCount }} 条受阻{{
                  state.demo ? " · 所有发送均为模拟" : ""
                }}</span
              >
            </div>
            <el-button
              size="small"
              :type="state.settings.send_paused ? 'primary' : ''"
              @click="toggleQueue"
              >{{
                state.settings.send_paused ? "恢复发送队列" : "暂停发送队列"
              }}</el-button
            >
          </div>
          <section class="panel table-panel">
            <el-tabs
              v-model="commentTab"
              @tab-click="
                pageNumber = 1;
                clearTableSelection();
              "
              ><el-tab-pane
                v-for="tab in commentTabs"
                :key="tab.key"
                :name="tab.key"
                ><span slot="label"
                  >{{ tab.label }}
                  <span class="tab-count">{{ tabCount(tab.key) }}</span></span
                ></el-tab-pane
              ></el-tabs
            >
            <div class="filter-bar">
              <el-input
                v-model="search"
                placeholder="搜索评论内容、帖子、账号"
                prefix-icon="el-icon-search"
                clearable
              ></el-input
              ><el-select v-model="taskFilter" clearable placeholder="全部任务"
                ><el-option
                  v-for="t in state.tasks"
                  :key="t.id"
                  :label="t.name"
                  :value="t.id"
                ></el-option
              ></el-select>
              <div class="grow"></div>
              <el-button
                size="small"
                icon="el-icon-download"
                @click="exportComments"
                >导出当前筛选</el-button
              >
            </div>
            <div v-if="selected.length" class="bulk-bar">
              已选择 {{ selected.length }} 条
              <el-button size="mini" :disabled="!!bulkBusy" @click="bulk('approve')"
                >批量通过</el-button
              ><el-button size="mini" :disabled="!!bulkBusy" @click="bulk('skip')">批量跳过</el-button
              ><el-button size="mini" :disabled="!!bulkBusy" @click="bulk('cancel')"
                >批量取消</el-button
              >
              <el-button size="mini" type="danger" plain :disabled="!!bulkBusy" @click="deleteSelected('comments')"
                >批量删除</el-button
              >
            </div>
            <el-table
              ref="commentsTable"
              :data="paged(filteredComments)"
              row-key="id"
              @selection-change="selected = $event"
              ><el-table-column
                type="selection"
                width="44"
                reserve-selection
                :selectable="
                  (row) =>
                    !['sent', 'sending', 'uncertain'].includes(row.status)
                "
              ></el-table-column
              ><el-table-column label="原帖 / 任务" min-width="230"
                ><template slot-scope="{ row }"
                  ><div class="post-title link" @click="showComment(row)">
                    {{ postFor(row).title || "图片帖（无文字标题）" }}
                  </div>
                  <small
                    >{{ taskName(row.task_id) }} ·
                    {{ postFor(row).author }}</small
                  ></template
                ></el-table-column
              ><el-table-column label="评论内容" min-width="280"
                ><template slot-scope="{ row }"
                  ><div class="comment-preview">
                    {{ row.sent_text || row.text }}
                  </div>
                  <small
                    >{{ row.strategy.name }} · v{{
                      row.strategy.version
                    }}</small
                  >
                  <div v-if="row.error" class="inline-error">
                    {{ row.error }}
                  </div></template
                ></el-table-column
              ><el-table-column label="状态" width="110"
                ><template slot-scope="{ row }"
                  ><el-tag size="small" :type="statusType(row.status)">{{
                    statusLabels[row.status]
                  }}</el-tag></template
                ></el-table-column
              ><el-table-column label="等待原因" min-width="230">
                <template slot-scope="{ row }">
                  <template v-if="row.status === 'queued'">
                    <div
                      :class="{ 'queue-blocked-text': !!queueBlockReason(row) }"
                    >
                      {{ queueReason(row) }}
                    </div>
                    <el-button
                      type="text"
                      size="mini"
                      @click="openTaskSending(row.task_id)"
                      >查看任务发送开关</el-button
                    >
                  </template>
                  <span v-else class="muted">—</span>
                </template> </el-table-column
              ><el-table-column label="操作" width="170" fixed="right"
                ><template slot-scope="{ row }"
                  ><el-button type="text" @click="showComment(row)">{{
                    row.status === "sent" ? "查看记录" : "查看 / 编辑"
                  }}</el-button
                  ><el-button
                    v-if="['review', 'draft', 'failed'].includes(row.status)"
                    type="text"
                    @click="commentAction(row, 'approve')"
                    >通过</el-button
                  ><el-button
                    v-if="row.status === 'queued'"
                    type="text"
                    @click="commentAction(row, 'cancel')"
                    >取消</el-button
                  ></template
                ></el-table-column
              ></el-table
            ><pagination
              :total="filteredComments.length"
              :page.sync="pageNumber"
            ></pagination>
          </section>
        </template>
        <template v-if="page === 'strategies'">
          <div class="info-strip">
            <i class="el-icon-info"></i>
            策略修改后自动递增版本；已有评论保留生成时的策略快照，重新生成可应用最新版本。
          </div>
          <div class="task-grid">
            <article
              v-for="s in state.strategies"
              :key="s.id"
              class="panel strategy-card"
            >
              <div class="panel-heading">
                <div class="task-symbol purple">
                  <i class="el-icon-magic-stick"></i>
                </div>
                <el-tag size="small" :type="s.enabled ? 'success' : 'info'">{{
                  s.enabled ? "已启用" : "已停用"
                }}</el-tag>
              </div>
              <h3>
                {{ s.name }} <small>v{{ s.version }}</small>
              </h3>
              <p>{{ s.prompt || s.fixed_text || "暂无描述" }}</p>
              <div class="source-tags">
                <el-tag size="mini" type="info">{{
                  s.mode === "ai" ? "AI 生成" : "固定话术"
                }}</el-tag
                ><el-tag size="mini" type="info"
                  >{{ s.max_length }} 字以内</el-tag
                >
              </div>
              <div class="resource-actions">
                <el-button size="small" @click="openForm('strategies', s)"
                  >编辑策略</el-button
                ><el-button
                  size="small"
                  @click="toggleResource('strategies', s)"
                  >{{ s.enabled ? "停用" : "启用" }}</el-button
                ><el-button
                  type="text"
                  class="danger"
                  @click="remove('strategies', s)"
                  >删除</el-button
                >
              </div>
            </article>
          </div>
          <el-empty
            v-if="!state.strategies.length"
            description="创建一套评论策略，统一管理回复风格"
          ></el-empty>
        </template>
        <template v-if="page === 'materials'">
          <div class="filter-bar">
            <el-input
              v-model="search"
              placeholder="搜索素材名称、分类、内容"
              prefix-icon="el-icon-search"
              clearable
            ></el-input
            ><span class="muted">启用的素材将作为所有任务的 AI 参考知识</span>
          </div>
          <div class="task-grid">
            <article
              class="panel material-card"
              v-for="m in filteredMaterials"
              :key="m.id"
            >
              <div class="panel-heading">
                <el-tag size="small" type="info">{{
                  m.category || "未分类"
                }}</el-tag
                ><el-switch
                  :value="m.enabled"
                  @change="toggleResource('materials', m)"
                ></el-switch>
              </div>
              <h3>{{ m.name }}</h3>
              <div
                v-if="referenceImage(m.image)"
                class="material-image-preview"
              >
                <el-image
                  :src="referenceImage(m.image)"
                  :preview-src-list="[referenceImage(m.image)]"
                  fit="contain"
                  alt="素材参考图片，点击查看大图"
                ></el-image>
              </div>
              <p>{{ m.content }}</p>
              <div v-if="m.image" class="material-image-notice">
                图片仅作参考，当前不支持图片/原生表情自动评论。
              </div>
              <div class="resource-actions">
                <el-button size="small" @click="openForm('materials', m)"
                  >编辑素材</el-button
                ><el-button
                  type="text"
                  class="danger"
                  @click="remove('materials', m)"
                  >删除</el-button
                >
              </div>
            </article>
          </div>
          <el-empty
            v-if="!filteredMaterials.length"
            description="添加常用知识或参考回复，让评论更贴近你的表达"
          ></el-empty>
        </template>
        <template v-if="page === 'models'">
          <div class="info-strip">
            <i class="el-icon-connection"></i> 支持 Chat Completions
            兼容接口，基础地址一般以 /v1 结尾。密钥保存后不会返回浏览器。
          </div>
          <section class="panel table-panel">
            <el-table :data="state.models"
              ><el-table-column
                prop="name"
                label="配置名称"
                min-width="150"
              ></el-table-column
              ><el-table-column
                prop="model"
                label="模型标识"
                min-width="180"
              ></el-table-column
              ><el-table-column
                prop="base_url"
                label="API 基础地址"
                min-width="240"
              ></el-table-column
              ><el-table-column label="API Key" width="120"
                ><template slot-scope="{ row }">{{
                  row.has_key ? "•••••• 已配置" : "未配置"
                }}</template></el-table-column
              ><el-table-column label="操作" width="230"
                ><template slot-scope="{ row }"
                  ><el-button type="text" @click="testModel(row)"
                    >测试连接</el-button
                  ><el-button type="text" @click="openForm('models', row)"
                    >编辑</el-button
                  ><el-button
                    type="text"
                    class="danger"
                    @click="remove('models', row)"
                    >删除</el-button
                  ></template
                ></el-table-column
              ></el-table
            >
          </section>
        </template>
        <template v-if="page === 'logs'">
          <section class="panel table-panel">
            <div class="filter-bar">
              <el-input
                v-model="search"
                placeholder="搜索日志内容"
                prefix-icon="el-icon-search"
                clearable
              ></el-input
              ><el-select v-model="taskFilter" clearable placeholder="全部任务"
                ><el-option
                  v-for="t in state.tasks"
                  :key="t.id"
                  :label="t.name"
                  :value="t.id"
                ></el-option></el-select
              ><span class="muted">保留最近 1000 条日志</span>
            </div>
            <el-table :data="paged(filteredLogs)"
              ><el-table-column label="时间" width="180"
                ><template slot-scope="{ row }">{{
                  date(row.at)
                }}</template></el-table-column
              ><el-table-column label="级别" width="100"
                ><template slot-scope="{ row }"
                  ><el-tag
                    size="mini"
                    :type="row.level === 'error' ? 'danger' : 'info'"
                    >{{ row.level }}</el-tag
                  ></template
                ></el-table-column
              ><el-table-column label="任务" width="190"
                ><template slot-scope="{ row }">{{
                  taskName(row.task_id)
                }}</template></el-table-column
              ><el-table-column
                prop="message"
                label="日志内容"
              ></el-table-column></el-table
            ><pagination
              :total="filteredLogs.length"
              :page.sync="pageNumber"
            ></pagination>
          </section>
        </template>
        <template v-if="page === 'settings'">
          <section class="panel settings-panel">
            <h3>发送设置</h3>
            <p class="muted">
              控制账号下的所有评论队列。每个任务仍受自身暂停状态与每轮发送上限约束。
            </p>
            <el-form label-position="top"
              ><el-form-item label="发送间隔（秒）"
                ><el-input-number
                  v-model="settingsForm.send_interval"
                  :min="10"
                  :max="86400"
                ></el-input-number></el-form-item
              ><el-form-item label="全局暂停发送"
                ><el-switch
                  v-model="settingsForm.send_paused"
                ></el-switch></el-form-item
              ><el-button type="primary" @click="saveSettings"
                >保存设置</el-button
              ></el-form
            ><el-divider></el-divider>
            <div class="panel-heading">
              <h3>本地图片 OCR</h3>
              <el-button
                size="small"
                :loading="ocrServiceBusy"
                @click="checkOCR"
                >检查连接</el-button
              >
            </div>
            <p class="muted">
              本地识别图片文字供文本模型分析；检查服务不会调用模型，状态仅手动刷新。
            </p>
            <dl v-if="ocrService" class="environment">
              <dt>服务状态</dt>
              <dd>
                <el-tag
                  size="small"
                  :type="
                    !ocrService.enabled
                      ? 'info'
                      : ocrService.available
                      ? 'success'
                      : 'warning'
                  "
                  >{{
                    !ocrService.enabled
                      ? "未启用"
                      : ocrService.available
                      ? "可用"
                      : "不可用"
                  }}</el-tag
                >
              </dd>
              <dt>识别引擎</dt>
              <dd>{{ ocrService.engine || "未配置" }}</dd>
              <dt v-if="ocrService.error">检查结果</dt>
              <dd v-if="ocrService.error" class="ocr-error-text">
                {{ ocrService.error }}
              </dd>
            </dl>
            <p v-else class="muted">尚未检查 OCR 服务。</p>
            <el-divider></el-divider>
            <h3>运行环境</h3>
            <dl class="environment">
              <dt>版本</dt>
              <dd>v{{ state.version }}</dd>
              <dt>运行模式</dt>
              <dd>
                {{
                  state.demo
                    ? "演示：模拟分析与发送，使用 MySQL 存储"
                    : "正式：连接小红书与已配置模型"
                }}
              </dd>
              <dt>数据存储</dt>
              <dd>MySQL · 演示与正式模式建议使用独立数据库</dd>
              <dt>浏览器</dt>
              <dd>内置 Chromium · go-rod · 代理及指纹沿用启动配置</dd>
              <dt>发送恢复</dt>
              <dd>服务重启后队列暂停，发送中记录转为待确认</dd>
            </dl>
          </section>
        </template>
        <footer>
          COMMENT STUDIO <span>小红书自动化评论管理 · v1.0.0</span>
        </footer>
      </main>
    </div>
    <resource-form
      :visible.sync="formVisible"
      :kind="formKind"
      :initial="formInitial"
      :state="state"
      :busy="saving"
      @save="saveForm"
    ></resource-form>
    <comment-drawer
      :visible.sync="detailVisible"
      :post="detailPost"
      :comment="detailComment"
      :related-comment="detailRelatedComment"
      :draft-max-length="draftMaxLength"
      :queue-reason="detailComment ? queueReason(detailComment) : ''"
      :task-name="taskName(detailPost && detailPost.task_id)"
      :demo="state.demo"
      :busy="saving"
      :reanalyzing="!!(detailPost && reanalyzing === detailPost.id)"
      @action="detailAction"
      @reanalyze="reanalyze"
      @save-draft="saveDraft"
      @view-comment="showComment"
      @view-task="openTaskSending"
    ></comment-drawer>
    <el-dialog title="小红书账号" :visible.sync="accountDialog" width="420px"
      ><div class="account-box">
        <div class="account-avatar"><i class="el-icon-user"></i></div>
        <h3>
          {{ accountText }}
        </h3>
        <p class="muted">
          {{ accountStatusDescription }}
        </p>
        <p v-if="account.id" class="muted">账号 ID：{{ account.id }}</p>
        <p v-if="account.checked_at" class="field-hint">
          最近检查：{{ date(account.checked_at) }}
        </p>
        <div v-if="account.error" class="inline-error">{{ account.error }}</div>
        <img
          v-if="qrcode"
          :src="qrcode"
          class="qrcode"
          alt="小红书登录二维码"
        /><el-button
          v-if="['offline', 'unknown', 'uncertain'].includes(accountPhase)"
          type="primary"
          :loading="accountBusy"
          @click="getQRCode"
          >获取登录二维码</el-button
        ><el-button :loading="accountBusy" @click="checkAccount"
          >检查登录状态（强验证）</el-button
        ><el-button v-if="account.logged_in && !state.demo" @click="logout"
          >退出登录</el-button
        >
        <p class="field-hint" v-if="state.demo">
          当前是体验账号，发送和模型分析均为本地模拟。
        </p>
      </div></el-dialog
    >
    <el-dialog title="访问凭证" :visible.sync="tokenDialog" width="450px"
      ><p class="muted">
        服务设置了 -token 或 AUTH_TOKEN 时填写。凭证仅保存在当前浏览器会话。
      </p>
      <el-input
        v-model="authToken"
        type="password"
        show-password
        placeholder="Bearer Token"
      ></el-input
      ><span slot="footer"
        ><el-button type="primary" @click="saveToken"
          >保存并连接</el-button
        ></span
      ></el-dialog
    >
  </div>
</template>

<script>
import Pagination from "./Pagination.vue";
import ResourceForm from "./ResourceForm.vue";
import CommentDrawer from "./CommentDrawer.vue";
import { referenceImage } from "./referenceImage";
import {
  emptyAccount,
  mergeAccountStatus,
  accountPhase as getAccountPhase,
} from "./accountState";
import {
  queueBlockReason as getQueueBlockReason,
  queueReason as getQueueReason,
} from "./queueStatus";
import {
  decisionLabel,
  decisionType,
  analysisModeLabel,
  analysisMethodLabel,
  ocrStatusLabel,
} from "./postAnalysis";
const emptyState = () => ({
  models: [],
  strategies: [],
  materials: [],
  tasks: [],
  posts: [],
  comments: [],
  logs: [],
  settings: { send_paused: true, send_interval: 60 },
});
const copy = (value) => JSON.parse(JSON.stringify(value));
export default {
  components: { Pagination, ResourceForm, CommentDrawer },
  data() {
    return {
      state: emptyState(),
      page: "overview",
      search: "",
      taskSearch: "",
      taskSubmitting: {},
      taskFilter: "",
      matchFilter: "",
      pageNumber: 1,
      commentTab: "review",
      selected: [],
      selectedPosts: [],
      postBatchProgress: "",
      bulkBusy: false,
      loading: false,
      saving: false,
      connectionError: "",
      lastRefresh: "",
      account: emptyAccount(),
      accountBusy: false,
      accountCacheBusy: false,
      accountStatusStarted: false,
      accountRequestVersion: 0,
      accountDialog: false,
      qrcode: "",
      tokenDialog: false,
      authToken: sessionStorage.getItem("console-token") || "",
      formVisible: false,
      formKind: "tasks",
      formInitial: {},
      detailVisible: false,
      detailPost: null,
      detailComment: null,
      reanalyzing: "",
      ocrService: null,
      ocrServiceBusy: false,
      settingsForm: { send_paused: true, send_interval: 60 },
      resourceNames: {
        tasks: "任务",
        models: "模型",
        strategies: "策略",
        materials: "素材",
      },
      sourceLabels: {
        keyword: "关键词",
        profile: "用户主页",
        tag: "话题词",
        link: "帖子链接",
      },
      statusLabels: {
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
      commentTabs: [
        { key: "review", label: "待审核" },
        { key: "queued", label: "发送队列" },
        { key: "sent", label: "已发送" },
        { key: "issues", label: "失败 / 待确认" },
        { key: "skipped", label: "已跳过 / 取消" },
        { key: "all", label: "全部评论" },
      ],
      menus: [
        {
          key: "overview",
          label: "工作台概览",
          en: "WORKSPACE OVERVIEW",
          icon: "el-icon-data-board",
        },
        {
          key: "tasks",
          label: "任务管理",
          en: "DISCOVERY TASKS",
          icon: "el-icon-connection",
        },
        {
          key: "posts",
          label: "候选帖子",
          en: "DISCOVERED POSTS",
          icon: "el-icon-document-copy",
        },
        {
          key: "comments",
          label: "评论管理",
          en: "COMMENT MANAGEMENT",
          icon: "el-icon-chat-dot-square",
        },
        {
          key: "strategies",
          label: "评论策略",
          en: "COMMENT STRATEGIES",
          icon: "el-icon-magic-stick",
        },
        {
          key: "materials",
          label: "素材库",
          en: "KNOWLEDGE LIBRARY",
          icon: "el-icon-collection",
        },
        {
          key: "models",
          label: "模型配置",
          en: "MODEL CONNECTIONS",
          icon: "el-icon-cpu",
        },
        {
          key: "logs",
          label: "运行日志",
          en: "ACTIVITY LOGS",
          icon: "el-icon-tickets",
        },
        {
          key: "settings",
          label: "系统设置",
          en: "WORKSPACE SETTINGS",
          icon: "el-icon-setting",
        },
      ],
      descriptions: {
        overview: "把内容发现、AI 分析和评论管理，连接成清晰的工作流。",
        tasks: "组合找帖渠道与分析目标，按自己的节奏运行。",
        posts: "结合正文和图片 OCR，查看文本模型的判断依据。",
        comments: "审核每一句回复，跟踪从草稿到发送的完整过程。",
        strategies: "统一回复风格，让每个任务都有合适的表达方式。",
        materials: "沉淀常用知识和参考回复，为 AI 提供更多上下文。",
        models: "连接自己的模型，自定义分析与评论生成能力。",
        logs: "追踪任务执行、评论操作与异常信息。",
        settings: "管理全局发送节奏，查看当前运行环境。",
      },
    };
  },
  computed: {
    accountPhase() {
      return getAccountPhase(this.account, this.accountBusy);
    },
    accountText() {
      if (this.accountPhase === "online") return this.account.name || "已登录";
      const label = {
        unknown: "账号待检查",
        checking: "检查登录中",
        uncertain: "状态待确认",
        offline: "已核实未登录",
      }[this.accountPhase];
      return this.account.name ? this.account.name + " · " + label : label;
    },
    accountStatusDescription() {
      return {
        unknown: "账号尚未核实。可手动检查登录状态，任务运行时也会实时核实。",
        checking: "正在检查登录状态，请稍候。",
        uncertain: "检查未成功，已保留已知账号信息，可稍后重试。",
        offline: "已核实当前未登录，可获取二维码后用小红书 App 扫码。",
        online: "登录状态已核实。",
      }[this.accountPhase];
    },
    blockedQueueCount() {
      return this.state.comments.filter(
        (comment) => !!getQueueBlockReason(comment, this.state)
      ).length;
    },
    detailRelatedComment() {
      return this.detailPost
        ? this.state.comments.find((c) => c.post_id === this.detailPost.id)
        : null;
    },
    draftMaxLength() {
      const task = this.state.tasks.find(
        (t) => t.id === this.detailPost?.task_id
      );
      const strategy = this.state.strategies.find(
        (s) => s.id === task?.strategy_id
      );
      return strategy?.max_length || 500;
    },
    currentMenu() {
      return this.menus.find((m) => m.key === this.page) || this.menus[0];
    },
    stats() {
      return [
        {
          label: "已发现帖子",
          value: this.state.posts.length,
          hint: "来自所有找帖渠道",
          icon: "el-icon-document-copy",
          color: "blue",
        },
        {
          label: "待审核评论",
          value: this.count("review"),
          hint: "等待你确认的回复",
          icon: "el-icon-edit-outline",
          color: "orange",
        },
        {
          label: "待发送评论",
          value: this.count("queued"),
          hint: this.state.settings.send_paused
            ? "队列当前已暂停"
            : this.blockedQueueCount
            ? "有 " + this.blockedQueueCount + " 条受阻，请检查任务开关"
            : "按发送间隔依次执行",
          icon: "el-icon-position",
          color: "purple",
        },
        {
          label: this.state.demo ? "模拟已发送" : "已发送评论",
          value: this.count("sent"),
          hint: "完整保留发送记录",
          icon: "el-icon-circle-check",
          color: "green",
        },
      ];
    },
    steps() {
      return [
        {
          title: "登录小红书账号",
          detail: "连接当前工作空间的操作账号",
          done: this.accountPhase === "online",
        },
        {
          title: "配置文本模型",
          detail: "用于判断正文与本地 OCR 提取的文字",
          done: !!this.state.models.length,
        },
        {
          title: "准备评论策略",
          detail: "设置风格、长度与参考素材",
          done: !!this.state.strategies.length,
        },
        {
          title: "创建并运行任务",
          detail: "选择找帖渠道，开始发现内容",
          done: this.state.tasks.some((t) => t.last_run),
        },
      ];
    },
    recentLogs() {
      return [...this.state.logs].reverse().slice(0, 5);
    },
    runningTaskCount() {
      return this.state.tasks.filter((task) => task.running).length;
    },
    queuedTaskCount() {
      return this.state.tasks.filter((task) => task.queued && !task.running)
        .length;
    },
    taskResultCounts() {
      const counts = {};
      for (const task of this.state.tasks) {
        counts[task.id] = { posts: 0, analyzed: 0, review: 0 };
      }
      for (const post of this.state.posts) {
        const task = counts[post.task_id];
        if (!task) continue;
        task.posts++;
        if (post.decision_status || post.match != null) task.analyzed++;
      }
      for (const comment of this.state.comments) {
        const task = counts[comment.task_id];
        if (task && this.inTab(comment, "review")) task.review++;
      }
      return counts;
    },
    filteredTasks() {
      const query = this.taskSearch.trim().toLowerCase();
      return this.state.tasks.filter((task) =>
        [task.name, task.goal, task.match_keywords, this.sourceSummary(task)]
          .join(" ")
          .toLowerCase()
          .includes(query)
      );
    },
    filteredPosts() {
      return [...this.state.posts]
        .reverse()
        .filter(
          (p) =>
            (!this.taskFilter || p.task_id === this.taskFilter) &&
            this.matches(
              [p.title, p.content, p.ocr_text, p.author_updates, p.author].join(
                " "
              )
            ) &&
            (!this.matchFilter ||
              (this.matchFilter === "review"
                ? p.decision_status === this.matchFilter
                : this.matchFilter === "pending"
                ? p.match == null && !p.decision_status
                : this.matchFilter === "yes"
                ? p.decision_status === "matched" ||
                  (!p.decision_status && p.match === true)
                : p.decision_status === "unmatched" ||
                  (!p.decision_status && p.match === false)))
        );
    },
    filteredComments() {
      return [...this.state.comments]
        .reverse()
        .filter(
          (c) =>
            (!this.taskFilter || c.task_id === this.taskFilter) &&
            this.inTab(c, this.commentTab) &&
            this.matches(
              c.text + " " + this.postFor(c).title + " " + c.account_id
            )
        );
    },
    filteredMaterials() {
      return this.state.materials.filter((m) =>
        this.matches(m.name + " " + m.category + " " + m.content)
      );
    },
    filteredLogs() {
      return [...this.state.logs]
        .reverse()
        .filter(
          (l) =>
            (!this.taskFilter || l.task_id === this.taskFilter) &&
            this.matches(l.message)
        );
    },
  },
  watch: {
    search() {
      this.pageNumber = 1;
      this.clearTableSelection();
    },
    taskFilter() {
      this.pageNumber = 1;
      this.clearTableSelection();
    },
    matchFilter() {
      this.pageNumber = 1;
      this.clearTableSelection();
    },
    pageNumber() {
      this.clearTableSelection();
    },
  },
  mounted() {
    this.restorePage();
    window.addEventListener("hashchange", this.restorePage);
    this.load();
    this.timer = setInterval(() => {
      if (!this.loading && !this.saving && !document.hidden) this.load(true);
    }, 5000);
    this.accountTimer = setInterval(() => {
      if (!document.hidden && this.accountStatusStarted && !this.state.demo)
        this.refreshAccountStatus();
    }, 60000);
    this.onVisibilityChange = () => {
      if (document.hidden) this.stopAccountPolling();
      else if (this.accountStatusStarted && !this.state.demo)
        this.refreshAccountStatus();
    };
    document.addEventListener("visibilitychange", this.onVisibilityChange);
  },
  beforeDestroy() {
    window.removeEventListener("hashchange", this.restorePage);
    clearInterval(this.timer);
    clearInterval(this.accountTimer);
    this.accountRequestVersion++;
    this.stopAccountPolling();
    document.removeEventListener("visibilitychange", this.onVisibilityChange);
  },
  methods: {
    referenceImage,
    decisionLabel,
    decisionType,
    analysisModeLabel,
    analysisMethodLabel,
    ocrStatusLabel,
    queueBlockReason(comment) {
      return getQueueBlockReason(comment, this.state);
    },
    queueReason(comment) {
      return getQueueReason(comment, this.state);
    },
    openTaskSending(taskID) {
      const task = this.state.tasks.find((item) => item.id === taskID);
      if (!task) {
        this.$message.warning("任务已不存在，请查看对应评论记录。");
        return;
      }
      this.detailVisible = false;
      this.navigate("tasks");
      this.taskSearch = task.name;
      this.$message.info(
        "已定位任务“" + task.name + "”，请确认任务开关和发送阶段开关。"
      );
    },
    openTaskResults(taskID, page) {
      this.navigate(page);
      this.taskFilter = taskID;
      this.matchFilter = "";
      if (page === "comments") this.commentTab = "review";
    },
    async api(path, method = "GET", body) {
      const res = await fetch("/api/console" + path, {
        method,
        headers: {
          "Content-Type": "application/json",
          ...(this.authToken
            ? { Authorization: "Bearer " + this.authToken }
            : {}),
        },
        ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
      });
      const httpError =
        res.status === 404
          ? "服务接口尚未就绪（HTTP 404），请刷新或稍后重试。"
          : "服务请求失败（HTTP " + res.status + "），请稍后重试。";
      let data;
      try {
        data = JSON.parse(await res.text());
      } catch {
        throw new Error(
          res.ok ? "服务返回格式异常，请刷新或稍后重试。" : httpError
        );
      }
      if (!res.ok) throw new Error(data?.error || data?.message || httpError);
      return data;
    },
    async load(silent = false) {
      if (!silent) this.loading = true;
      try {
        const data = await this.api("/state");
        this.state = data;
        this.connectionError = "";
        if (this.page !== "settings" || !this.lastRefresh)
          this.settingsForm = copy(data.settings);
        this.lastRefresh = new Date().toLocaleTimeString("zh-CN", {
          hour12: false,
        });
        if (data.demo) {
          this.account = {
            id: "demo-account",
            name: "体验账号",
            logged_in: true,
            checked_at: Math.floor(Date.now() / 1000),
            checking: false,
            error: "",
          };
        } else if (!this.accountStatusStarted) {
          this.accountStatusStarted = true;
          this.refreshAccountStatus();
        }
        if (this.detailComment) {
          const c = data.comments.find((c) => c.id === this.detailComment.id);
          if (c) {
            this.detailComment = c;
            this.detailPost = this.postFor(c);
          }
        } else if (this.detailPost) {
          this.detailPost =
            data.posts.find((p) => p.id === this.detailPost.id) ||
            this.detailPost;
        }
      } catch (e) {
        this.connectionError = e.message;
      } finally {
        this.loading = false;
      }
    },
    async mutate(path, body = {}, method = "POST") {
      try {
        await this.api(path, method, body);
        await this.load(true);
        return true;
      } catch (e) {
        this.$message.error(e.message);
        return false;
      }
    },
    restorePage() {
      const key = window.location.hash.slice(1);
      const page = this.menus.some((menu) => menu.key === key) ? key : "overview";
      if (page !== this.page) this.navigate(page, false);
    },
    navigate(key, updateURL = true) {
      this.clearTableSelection();
      this.page = key;
      if (updateURL && window.location.hash !== "#" + key)
        window.location.hash = key;
      this.search = "";
      this.taskSearch = "";
      this.taskFilter = "";
      this.pageNumber = 1;
      if (key === "settings") {
        this.settingsForm = copy(this.state.settings);
        if (!this.ocrService) this.checkOCR();
      }
    },
    matches(text) {
      return String(text || "")
        .toLowerCase()
        .includes(this.search.toLowerCase());
    },
    paged(rows) {
      const n = Math.min(
        this.pageNumber,
        Math.max(1, Math.ceil(rows.length / 15))
      );
      return rows.slice((n - 1) * 15, n * 15);
    },
    count(status) {
      return this.state.comments.filter((c) => c.status === status).length;
    },
    inTab(c, tab) {
      return (
        tab === "all" ||
        (tab === "issues"
          ? ["failed", "uncertain"].includes(c.status)
          : tab === "skipped"
          ? ["skipped", "cancelled"].includes(c.status)
          : tab === "queued"
          ? ["queued", "sending"].includes(c.status)
          : tab === "review"
          ? ["review", "draft"].includes(c.status)
          : c.status === tab)
      );
    },
    tabCount(tab) {
      return this.state.comments.filter((c) => this.inTab(c, tab)).length;
    },
    date(value) {
      return value
        ? new Date(value * 1000).toLocaleString("zh-CN", { hour12: false })
        : "—";
    },
    taskName(id) {
      return (
        this.state.tasks.find((t) => t.id === id)?.name ||
        (id ? "已删除任务" : "系统")
      );
    },
    taskStatus(t) {
      return t.running
        ? "运行中"
        : t.queued
        ? "排队中"
        : t.paused
        ? "已暂停"
        : t.last_error || t.phase === "failed"
        ? "上轮失败"
        : t.scheduled
        ? "定时运行"
        : "已就绪";
    },
    taskStatusType(t) {
      return t.running
        ? ""
        : t.queued
        ? "warning"
        : t.paused
        ? "info"
        : t.last_error || t.phase === "failed"
        ? "danger"
        : "success";
    },
    taskRunLabel(t) {
      return t.running ? "运行中" : t.queued ? "已排队" : "运行一次";
    },
    taskPhaseLabel(t) {
      if (!t.running && !t.queued && (!t.phase || t.phase === "idle")) {
        if (t.last_error) return "上轮失败";
        if (t.last_run) return "上轮已结束";
        if (t.paused) return "任务已暂停";
      }
      const labels = {
        queued: "等待执行",
        waiting_browser: "等待浏览器",
        account: "核实账号",
        discover: "获取帖子",
        detail: "读取详情",
        ocr: "识别图片文字",
        analyze: "分析内容",
        completed: "本轮完成",
        cancelled: "本轮已停止",
        failed: "运行失败",
        paused: "任务已暂停",
        idle: "尚未运行",
        manual: "核实人工草稿",
        reanalyze: "重新分析",
      };
      return (
        labels[t.phase] ||
        (t.running
          ? "任务执行中"
          : t.queued
          ? "等待执行"
          : t.paused
          ? "任务已暂停"
          : t.last_run
          ? "上轮已结束"
          : "尚未运行")
      );
    },
    taskProgress(t) {
      if (
        !t.running &&
        !t.queued &&
        (!t.phase || t.phase === "idle") &&
        t.last_error
      )
        return t.last_error;
      if (t.progress) return t.progress;
      if (t.queued) return "已加入队列，等待可用执行位置。";
      if (t.running) return "正在处理，阶段与找到的帖子会自动更新。";
      if (t.last_error) return t.last_error;
      if (t.paused) return "任务已暂停，可查看已有帖子与运行日志。";
      if (t.last_run) return "可查看累计找到的帖子及分析结果。";
      return "点击“运行一次”开始找帖；评论仍按审核与发送开关处理。";
    },
    sourceSummary(t) {
      return (t.sources || [])
        .map((s) => this.sourceLabels[s.kind] + "：" + s.value)
        .join(" / ");
    },
    statusType(s) {
      return s === "sent"
        ? "success"
        : ["failed", "uncertain"].includes(s)
        ? "danger"
        : ["review", "draft"].includes(s)
        ? "warning"
        : s === "queued"
        ? ""
        : "info";
    },
    postFor(c) {
      return this.state.posts.find((p) => p.id === c.post_id) || {};
    },
    openForm(kind, row) {
      const defaults = {
        models: { name: "", base_url: "", model: "", api_key: "" },
        strategies: {
          name: "",
          mode: "ai",
          enabled: true,
          prompt: "根据帖子中的具体问题，提供简短、自然、有帮助的回复。",
          fixed_text: "",
          max_length: 120,
          forbidden: "",
        },
        materials: {
          name: "",
          category: "知识",
          content: "",
          image: "",
          enabled: true,
        },
        tasks: {
          name: "",
          sources: [{ kind: "keyword", value: "" }],
          model_id: this.state.models[0]?.id || "",
          analysis_mode: "model",
          match_keywords: "",
          demand_keywords: "",
          strategy_id: this.state.strategies.find((s) => s.enabled)?.id || "",
          goal: "",
          exclude: "",
          publish_time: "不限",
          note_type: "不限",
          interval: 300,
          limit: 10,
          send_limit: 3,
          auto_send: false,
          scheduled: false,
          collect_paused: false,
          analyze_paused: false,
          send_paused: false,
        },
      };
      this.formKind = kind;
      this.formInitial = copy(row || defaults[kind]);
      this.formVisible = true;
    },
    async saveForm(form) {
      this.saving = true;
      if (await this.mutate("/" + this.formKind, form)) {
        this.formVisible = false;
        this.$message.success("已保存");
        this.navigate(this.formKind);
      }
      this.saving = false;
    },
    async reanalyze(post) {
      if (this.reanalyzing || this.bulkBusy) return;
      this.reanalyzing = post.id;
      try {
        if (await this.mutate("/posts/" + post.id + "/reanalyze")) {
          this.$message.success(
            "该帖详情、OCR 和筛选已更新，未生成或发送评论。"
          );
        }
      } finally {
        this.reanalyzing = "";
      }
    },
    clearTableSelection() {
      this.$refs.postsTable?.clearSelection();
      this.$refs.commentsTable?.clearSelection();
      this.selectedPosts = [];
      this.selected = [];
    },
    async reanalyzeSelected() {
      if (this.bulkBusy || this.reanalyzing || !this.selectedPosts.length) return;
      const posts = [...this.selectedPosts];
      let done = 0;
      const errors = [];
      this.bulkBusy = true;
      try {
        for (const [index, post] of posts.entries()) {
          this.postBatchProgress = "正在分析第 " + (index + 1) + " / " + posts.length + " 条";
          this.reanalyzing = post.id;
          try {
            await this.api("/posts/" + encodeURIComponent(post.id) + "/reanalyze", "POST");
            done++;
          } catch (e) {
            errors.push(e.message);
          }
        }
        await this.load(true);
        this.clearTableSelection();
        if (errors.length) {
          this.$message.warning("已处理 " + done + " 条，失败 " + errors.length + " 条：" + errors[0]);
        } else {
          this.$message.success("已处理 " + done + " 条候选帖子，识别结果见列表");
        }
      } finally {
        this.bulkBusy = false;
        this.reanalyzing = "";
        this.postBatchProgress = "";
      }
    },
    async deleteSelected(kind) {
      if (this.bulkBusy || this.reanalyzing) return;
      const isPosts = kind === "posts";
      const ids = (isPosts ? this.selectedPosts : this.selected).map((row) => row.id);
      if (!ids.length) return;
      const name = isPosts ? "候选帖子" : "评论记录";
      const note = isPosts ? "有关联评论的帖子会保留。" : "已发送、发送中和待确认的记录会保留。";
      this.bulkBusy = true;
      try {
        try {
          await this.$confirm("从工作台删除所选 " + ids.length + " 条" + name + "？" + note, "批量删除", { type: "warning" });
        } catch {
          return;
        }
        const { deleted, protected: retained } = await this.api("/" + kind + "/bulk-delete", "POST", { ids });
        await this.load(true);
        if (
          this.detailVisible &&
          (isPosts
            ? this.detailPost && !this.state.posts.some((p) => p.id === this.detailPost.id)
            : this.detailComment && !this.state.comments.some((c) => c.id === this.detailComment.id))
        ) this.detailVisible = false;
        const total = isPosts ? this.filteredPosts.length : this.filteredComments.length;
        this.pageNumber = Math.min(this.pageNumber, Math.max(1, Math.ceil(total / 15)));
        this.clearTableSelection();
        const missing = ids.length - deleted - retained;
        const message = "已删除 " + deleted + " 条" +
          (retained ? "，保留 " + retained + " 条受保护记录" : "") +
          (missing > 0 ? "，" + missing + " 条已不存在" : "");
        if (retained || missing > 0) this.$message.warning(message);
        else this.$message.success(message);
      } catch (e) {
        this.$message.error(e.message);
      } finally {
        this.bulkBusy = false;
      }
    },
    async saveDraft(text) {
      if (!this.detailPost || this.saving) return;
      const postID = this.detailPost.id;
      this.saving = true;
      try {
        if (await this.mutate("/posts/" + postID + "/draft", { text })) {
          const comment = this.state.comments.find((c) => c.post_id === postID);
          if (comment) this.showComment(comment);
          this.$message.success("人工草稿已保存为待审核，未发送评论。");
        }
      } finally {
        this.saving = false;
      }
    },
    async checkOCR() {
      if (this.ocrServiceBusy) return;
      this.ocrServiceBusy = true;
      try {
        this.ocrService = await this.api("/ocr/status");
      } catch (e) {
        this.ocrService = {
          enabled: false,
          available: false,
          engine: "",
          error: e.message,
        };
      } finally {
        this.ocrServiceBusy = false;
      }
    },
    async toggleResource(kind, row) {
      await this.mutate("/" + kind, { ...row, enabled: !row.enabled });
    },
    async remove(kind, row) {
      try {
        await this.$confirm(
          "删除“" + row.name + "”？历史评论记录会保留。",
          "删除确认",
          { type: "warning" }
        );
        if (await this.mutate("/" + kind + "/" + row.id, undefined, "DELETE"))
          this.$message.success("已删除");
      } catch {}
    },
    async taskAction(t, action) {
      if (
        this.taskSubmitting[t.id] ||
        (action === "run" && (t.running || t.queued))
      )
        return;
      this.$set(this.taskSubmitting, t.id, action);
      try {
        if (await this.mutate("/tasks/" + t.id + "/" + action)) {
          const current =
            this.state.tasks.find((task) => task.id === t.id) || t;
          this.$message.success(
            action !== "run"
              ? "任务状态已更新"
              : current.queued
              ? "任务已排队，可在任务卡查看进度。"
              : current.running
              ? "任务已开始运行，可查看阶段与结果。"
              : "任务已提交，进度和结果将自动更新。"
          );
        }
      } finally {
        this.$delete(this.taskSubmitting, t.id);
      }
    },
    taskCommand(t, command) {
      if (command === "edit") this.openForm("tasks", t);
      else this.remove("tasks", t);
    },
    showPost(p) {
      this.detailPost = copy(p);
      this.detailComment = null;
      this.detailVisible = true;
    },
    showComment(c) {
      this.detailComment = copy(c);
      this.detailPost = this.postFor(c);
      this.detailVisible = true;
    },
    async commentAction(c, action, text) {
      return this.mutate("/comments/" + c.id + "/" + action, {
        text: text || "",
      });
    },
    async detailAction(action, text) {
      this.saving = true;
      try {
        if (action === "approve" && text !== this.detailComment.text) {
          if (!(await this.commentAction(this.detailComment, "edit", text)))
            return;
        }
        const body =
          action === "regenerate"
            ? { instruction: text || "" }
            : { text: text || "" };
        if (
          await this.mutate(
            "/comments/" + this.detailComment.id + "/" + action,
            body
          )
        )
          this.$message.success(
            action === "regenerate" ? "已生成新稿，请审核" : "评论已更新"
          );
      } finally {
        this.saving = false;
      }
    },
    async bulk(action) {
      if (this.bulkBusy || !this.selected.length) return;
      this.bulkBusy = true;
      try {
        let ok = 0;
        const errors = [];
        for (const c of [...this.selected]) {
          try {
            await this.api("/comments/" + c.id + "/" + action, "POST", {});
            ok++;
          } catch (e) {
            errors.push(e.message);
          }
        }
        await this.load(true);
        this.selected = [];
        this.$refs.commentsTable?.clearSelection();
        if (errors.length)
          this.$message.warning(
            "完成 " + ok + " 条，未处理 " + errors.length + " 条：" + errors[0]
          );
        else this.$message.success("已处理 " + ok + " 条");
      } finally {
        this.bulkBusy = false;
      }
    },
    async toggleQueue() {
      const next = {
        ...this.state.settings,
        send_paused: !this.state.settings.send_paused,
      };
      if (await this.mutate("/settings", next))
        this.$message.success(
          next.send_paused ? "发送队列已暂停" : "发送队列已恢复"
        );
    },
    async saveSettings() {
      if (await this.mutate("/settings", this.settingsForm))
        this.$message.success("设置已保存");
    },
    async testModel(m) {
      const toast = this.$message({
        message: "正在测试模型连接…",
        duration: 0,
      });
      try {
        await this.api("/models/" + m.id + "/test", "POST", {});
        this.$message.success(
          this.state.demo ? "演示模式：连接测试已模拟通过" : "模型连接成功"
        );
      } catch (e) {
        this.$message.error(e.message);
      } finally {
        toast.close();
      }
    },
    openAccount() {
      this.accountDialog = true;
      this.refreshAccountStatus();
    },
    stopAccountPolling() {
      clearTimeout(this.accountPollTimer);
      this.accountPollTimer = null;
      this.accountPollUntil = 0;
    },
    scheduleAccountPolling(result) {
      clearTimeout(this.accountPollTimer);
      this.accountPollTimer = null;
      const waiting = result.checking === true;
      if (!waiting || document.hidden || this.state.demo) {
        this.stopAccountPolling();
        return;
      }
      if (!this.accountPollUntil) this.accountPollUntil = Date.now() + 45000;
      if (Date.now() >= this.accountPollUntil) {
        this.account = {
          ...this.account,
          checking: false,
          error: "登录检查耗时较长，状态待确认；可手动检查。",
        };
        return;
      }
      this.accountPollTimer = setTimeout(
        () => this.refreshAccountStatus(true),
        2000
      );
    },
    async refreshAccountStatus(polling = false) {
      if (
        this.state.demo ||
        this.accountBusy ||
        this.accountCacheBusy ||
        document.hidden
      )
        return;
      if (
        !polling &&
        (!this.accountPollUntil || Date.now() >= this.accountPollUntil)
      )
        this.accountPollUntil = Date.now() + 45000;
      if (polling && Date.now() >= this.accountPollUntil) {
        this.scheduleAccountPolling(this.account);
        return;
      }
      this.accountCacheBusy = true;
      const requestVersion = ++this.accountRequestVersion;
      try {
        const result = await this.api("/account/status");
        if (requestVersion === this.accountRequestVersion) {
          this.account = mergeAccountStatus(this.account, result);
          if (getAccountPhase(this.account) === "online") this.qrcode = "";
          this.scheduleAccountPolling(result);
        }
      } catch (e) {
        if (requestVersion === this.accountRequestVersion) {
          this.account = mergeAccountStatus(this.account, {
            error: e.message,
            checked_at: this.account.checked_at,
          });
          this.stopAccountPolling();
        }
      } finally {
        this.accountCacheBusy = false;
      }
    },
    async checkAccount() {
      this.stopAccountPolling();
      this.accountBusy = true;
      const requestVersion = ++this.accountRequestVersion;
      try {
        const result = await this.api("/account?fresh=true");
        if (requestVersion === this.accountRequestVersion) {
          this.account = mergeAccountStatus(this.account, result, true);
          if (getAccountPhase(this.account) === "online") this.qrcode = "";
          this.scheduleAccountPolling(this.account);
        }
      } catch (e) {
        if (requestVersion === this.accountRequestVersion)
          this.account = mergeAccountStatus(this.account, {
            error: e.message,
            checked_at: this.account.checked_at,
          });
        this.$message.error(e.message);
      } finally {
        this.accountBusy = false;
      }
    },
    async getQRCode() {
      this.accountBusy = true;
      try {
        const data = await this.api("/account/qrcode");
        if (data.is_logged_in) {
          await this.checkAccount();
        } else if (data.img)
          this.qrcode = data.img.startsWith("data:")
            ? data.img
            : "data:image/png;base64," + data.img;
      } catch (e) {
        this.$message.error(e.message);
      } finally {
        this.accountBusy = false;
      }
    },
    async logout() {
      try {
        await this.$confirm("退出会暂停所有任务与发送队列。", "退出登录");
        this.accountRequestVersion++;
        if (await this.mutate("/account/logout")) {
          this.accountRequestVersion++;
          this.stopAccountPolling();
          this.account = {
            ...emptyAccount(),
            logged_in: false,
            checked_at: Math.floor(Date.now() / 1000),
          };
          this.qrcode = "";
        }
      } catch {}
    },
    saveToken() {
      sessionStorage.setItem("console-token", this.authToken);
      this.tokenDialog = false;
      this.load();
    },
    stepAction(i) {
      if (i === 0) {
        this.openAccount();
      } else this.navigate(["", "models", "strategies", "tasks"][i]);
    },
    async exportComments() {
      try {
        const rows = this.filteredComments;
        const csvCell = (value) => {
          let text = String(value ?? "");
          if (/^[=+\-@\t\r]/.test(text.trimStart())) text = "'" + text;
          return '"' + text.replace(/"/g, '""') + '"';
        };
        const table = [
          [
            "评论ID",
            "任务",
            "帖子标题",
            "原帖链接",
            "账号",
            "状态",
            "AI原稿",
            "当前内容",
            "实际发送",
            "发送时间",
            "错误",
          ],
          ...rows.map((c) => [
            c.id,
            this.taskName(c.task_id),
            this.postFor(c).title,
            this.postFor(c).url,
            c.account_id,
            this.statusLabels[c.status],
            c.original,
            c.text,
            c.sent_text,
            this.date(c.sent_at),
            c.error,
          ]),
        ];
        const blob = new Blob(
          [
            "\ufeff" +
              table.map((row) => row.map(csvCell).join(",")).join("\r\n"),
          ],
          { type: "text/csv;charset=utf-8" }
        );
        const url = URL.createObjectURL(blob);
        const a = document.createElement("a");
        a.href = url;
        a.download = "comments-v1.0.0.csv";
        a.click();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
      } catch (e) {
        this.$message.error(e.message);
      }
    },
  },
};
</script>
