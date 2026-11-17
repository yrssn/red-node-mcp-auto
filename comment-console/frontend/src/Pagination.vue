<template>
  <div class="pagination">
    <span>共 {{ total }} 条</span
    ><el-pagination
      background
      layout="prev, pager, next"
      :total="total"
      :page-size="15"
      :current-page="page"
      @current-change="$emit('update:page', $event)"
    ></el-pagination>
  </div>
</template>
<script>
export default { props: ["total", "page"] };
</script>
