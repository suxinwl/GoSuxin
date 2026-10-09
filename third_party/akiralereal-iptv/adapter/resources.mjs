// SPDX-License-Identifier: GPL-3.0-only
import { mkdirSync, openSync, writeFileSync, closeSync, readFileSync, existsSync, unlinkSync, statSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { join } from 'node:path';

const alive=pid=>{try{process.kill(pid,0);return true;}catch(error){return error.code!=='ESRCH';}};
const read=path=>{try{return JSON.parse(readFileSync(path,'utf8'));}catch{return null;}};
const pause=ms=>new Promise(resolve=>setTimeout(resolve,ms));
export function createResourceBudget(directory,{browser=2,source=4}={}) {
  mkdirSync(directory,{recursive:true,mode:0o700});
  const held=new Set(),limits={browser,source};
  function clearStale(path) {
    const previous=read(path);
    if(previous?.pid&&alive(previous.pid))return;
    // An incomplete creation belongs to its writer for a short grace period.
    if(!previous&&Date.now()-statSync(path).mtimeMs<30000)return;
    const guard=path+'.cleanup';let descriptor;
    try {descriptor=openSync(guard,'wx',0o600);writeFileSync(descriptor,JSON.stringify({pid:process.pid}));}
    catch(error){if(error.code==='EEXIST'){const owner=read(guard);if(owner?.pid&&!alive(owner.pid)){try{unlinkSync(guard);}catch{}}return;}throw error;}
    try {
      const current=read(path);
      if(current?.id===previous?.id&&(!current?.pid||!alive(current.pid)))try{unlinkSync(path);}catch{}
    } finally {closeSync(descriptor);try{unlinkSync(guard);}catch{}}
  }
  async function acquire(kind,{waitMs=60000}={}) {
    if(!limits[kind])throw new Error('unknown resource budget');
    const deadline=Date.now()+waitMs;
    while(true) {
      for(let index=0;index<limits[kind];index++) {
        const path=join(directory,`${kind}-${index}.json`),id=randomUUID();
        if(existsSync(path+'.cleanup'))continue;
        let descriptor;
        try {
          descriptor=openSync(path,'wx',0o600);writeFileSync(descriptor,JSON.stringify({pid:process.pid,id,kind,created_at:new Date().toISOString()}));closeSync(descriptor);descriptor=undefined;
          let released=false;
          const release=()=>{if(released)return;released=true;held.delete(release);if(read(path)?.id===id)try{unlinkSync(path);}catch{}};
          held.add(release);return release;
        } catch(error) {
          if(descriptor!==undefined)closeSync(descriptor);
          if(error.code!=='EEXIST')throw error;
          try{clearStale(path);}catch(error){if(error.code!=='ENOENT')throw error;}
        }
      }
      if(Date.now()>=deadline)throw new Error(`host ${kind} capacity reached; retry later`);
      await pause(Math.min(100,Math.max(1,deadline-Date.now())));
    }
  }
  return {directory,acquire,async run(kind,task,options){const release=await acquire(kind,options);try{return await task();}finally{release();}},
    close(){for(const release of [...held])release();}};
}

export async function installBrowserBudget(budget,limit=2) {
  const {getBrowserPool}=await import('../utils/browserLauncher.js');
  const pool=getBrowserPool(),acquire=pool.acquire.bind(pool);
  pool.limit=Math.min(2,limit);
  pool.acquire=async options=>{
    const slot=await acquire(options);let release;
    try{release=await budget.acquire('browser',{waitMs:options?.waitMs||60000});}
    catch(error){slot.release();throw error;}
    const localRelease=slot.release;
    slot.release=()=>{localRelease();release();};
    return slot;
  };
  // Set the original bridge's configurable instance property before runtime.js
  // constructs it. The pinned source and its upstream constant remain intact.
  const {VipMseBridge}=await import('../extractors/yangshipin/vip-bridge.js');
  const property=Symbol('adapterVipLimit');
  Object.defineProperty(VipMseBridge.prototype,'maxActiveChannels',{configurable:true,
    get(){return this[property]||2;},set(value){this[property]=Math.max(1,Math.min(2,Number(value)||2));}});
}
