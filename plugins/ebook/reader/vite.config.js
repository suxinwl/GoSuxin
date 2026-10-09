import { defineConfig } from 'vite';
export default defineConfig({base:'/albums/',resolve:{alias:{vue:'vue/dist/vue.esm-bundler.js'}},define:{__VUE_OPTIONS_API__:true,__VUE_PROD_DEVTOOLS__:false},build:{outDir:'../../../resource/plugins/ebook/reader',emptyOutDir:true}});
