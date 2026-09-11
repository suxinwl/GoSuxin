/// <reference types="vite/client" />

declare module "*.vue" {
  import type { DefineComponent } from "vue";
  const component: DefineComponent<{}, {}, any>;
  export default component;
}
interface ImportMetaEnv {
  readonly VITE_API_BASE_URL: string;
}
declare interface Window {
  globalConfig: any
}
declare module '@arco-design/color'
declare module '@umoteam/editor'