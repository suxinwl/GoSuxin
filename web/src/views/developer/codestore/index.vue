<template>
  <div class="container">
    <page-card breadcrumb scrollPage>
      <a-result status="info" title="Suxin 插件市场筹备中">
        <template #subtitle>插件市场与购买服务筹备中，将迁移至 Suxin 官网。</template>
        <template #extra><a-link href="https://www.suxinwl.com" target="_blank" rel="noopener noreferrer">访问 Suxin 官网</a-link></template>
      </a-result>
      <a-space>
        <a-button type="primary" @click="openPackModal(true, {record:{domian:'',code_token:'',tabwarehouse:'local'}})">本地代码打包</a-button>
        <a-upload accept=".zip" :show-file-list="false" :custom-request="handleInstallLLocalCode"><template #upload-button><a-button>安装本地插件</a-button></template></a-upload>
      </a-space>
    </page-card>
    <PackUpCode @register="registerPackModal" />
  </div>
</template>
<script setup lang="ts">
import type { RequestOption } from '@arco-design/web-vue/es/upload/interfaces';
import { Message } from '@arco-design/web-vue';
import { useModal } from '/@/components/Modal';
import { installLocalCode, installCode } from '@/api/developer/packinstall';
import PackUpCode from './PackUpCode.vue';
const [registerPackModal, { openModal: openPackModal }] = useModal();
   const handleInstallLLocalCode = (options: RequestOption) => {
      const controller = new AbortController();
        (async function requestWrap() {
          const {
            onProgress,
            onError,
            fileItem,
          } = options;
          onProgress(20);
          const onUploadProgress = (event: ProgressEvent) => {
            let percent;
            if (event.total > 0) {
              percent = (event.loaded / event.total) * 100;
            }
            onProgress(parseInt(String(percent), 10), event);
          };
          try {
            //开始手动上传
            Message.loading({content:"上传本地代码中",id:"upkey",duration:0})
            const filename=fileItem?.name||""
            const resdata = await installLocalCode({ name: 'file', file: fileItem.file as Blob, filename,data:{}},onUploadProgress);
            if(resdata){
              if(resdata["code"]==0){
                  Message.success({content:resdata["message"],id:"upkey",duration:2000})
                  if (resdata){
                    Message.loading({content:"安装中",id:"upkey",duration:0})
                    await installCode({name:resdata.data});
                    Message.success({content:"安装成功",id:"upkey",duration:2000})
                  }else{
                    Message.error({content:"已经安装",id:"upkey",duration:2000})
                  }
              }else{
                Message.error({content:resdata["message"],id:"upkey",duration:2000})
              }
            }else{
              Message.error({content:resdata["message"],id:"upkey",duration:2000})
            }
          } catch (error) {
            onError(error);
            Message.error({content:"上传失败",id:"upkey",duration:2000})
          }
        })();
        return {
          abort() {
            controller.abort();
          },
        };
    };
</script>
