import { createRouter, createWebHashHistory } from "vue-router";
import Home from "@/views/Home.vue";
import Config from "@/views/Config.vue";
import ModelConfig from "@/views/ModelConfig.vue";
import ModelEditor from "@/views/ModelEditor.vue";

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    {
      path: "/",
      component: Home,
      meta: { showIcon: true, title: "", directlyClose: false },
    },
    {
      path: "/config",
      component: Config,
      meta: { showIcon: false, title: "设置", directlyClose: true },
    },
    {
      path: "/model-config",
      component: ModelConfig,
      meta: { showIcon: false, title: "模型配置", directlyClose: true },
    },
    {
      path: "/model-editor",
      component: ModelEditor,
      meta: { showIcon: false, title: "模型编辑", directlyClose: true },
    },
  ],
});

export default router;
