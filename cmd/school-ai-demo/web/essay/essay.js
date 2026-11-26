(() => {
'use strict';
const $ = id => document.getElementById(id);
const API = '/ai-demo/api/essay-review';
let mode='text', busy=false, preparing=false, available=false, remaining=0, imageSupported=true;
let selected=null, previewURL='', generation=0, output='', model='deepseek-v4.1-flash', toastTimer;
function error(message=''){ $('error').textContent=message; $('error').hidden=!message; }
function toast(message){$('toast').textContent=message;$('toast').hidden=false;clearTimeout(toastTimer);toastTimer=setTimeout(()=>{$('toast').hidden=true;},2400);}
async function copy(text){
 try {await navigator.clipboard.writeText(text);} catch(_){const box=document.createElement('textarea');box.value=text;box.style.cssText='position:fixed;left:-10000px';document.body.append(box);box.select();const ok=document.execCommand('copy');box.remove();if(!ok){toast('无法自动复制，请手动选择文字复制。');return;}}
 toast('已复制');
}
$('copyPrompt').onclick=()=>copy($('promptText').textContent);
$('copyResult').onclick=()=>copy(output);
function refresh(){
 const count=[...$('essayText').value.trim()].length;
 $('textCount').textContent=`${count} / 8000 字`;$('textCount').classList.toggle('over',count>8000);
 const valid=mode==='text'?count>0&&count<=8000:!!selected&&imageSupported;
 $('submitBtn').disabled=busy||preparing||!available||remaining<=0||!valid||!$('consent').checked;
 $('submitBtn').textContent=busy?'生成中…':'生成点评';
 for(const id of ['textTab','imageTab','essayText','imageFile','removeImage','consent'])$(id).disabled=busy;
 $('requestStatus').textContent=busy?'正在分析，最长约 120 秒，请勿重复提交':preparing?'正在处理图片…':mode==='image'&&!imageSupported?'当前模型暂时不能分析图片，请切换文字模式':remaining<=0&&available?'今日额度已用完':!available?'等待模型服务就绪':`今日剩余 ${remaining} 次 · 一次一个优点、一个建议`;
}
function clearResult(){output='';$('copyResult').hidden=true;$('resultBox').classList.remove('ready');$('resultTitle').textContent='点评会出现在这里';$('reviewText').textContent='提交当前作文后，这里会显示新的实时点评。';$('reviewCount').textContent='';}
function removeImage(){generation++;preparing=false;selected=null;if(previewURL)URL.revokeObjectURL(previewURL);previewURL='';$('imagePreview').removeAttribute('src');$('imageFile').value='';$('previewBox').hidden=true;refresh();}
function setMode(next){if(busy)return;mode=next;removeImage();$('essayText').value='';error();clearResult();for(const kind of ['text','image']){const active=kind===next;$(kind+'Tab').setAttribute('aria-selected',String(active));$(kind+'Tab').tabIndex=active?0:-1;$(kind+'Panel').hidden=!active;}refresh();}
for(const kind of ['text','image']){
 $(kind+'Tab').onclick=()=>setMode(kind);
 $(kind+'Tab').onkeydown=e=>{if(['ArrowLeft','ArrowRight','Home','End'].includes(e.key)){e.preventDefault();const next=e.key==='Home'?'text':e.key==='End'?'image':mode==='text'?'image':'text';setMode(next);$(next+'Tab').focus();}};
}
$('essayText').oninput=()=>{error();clearResult();refresh();};$('consent').onchange=refresh;
$('removeImage').onclick=()=>{removeImage();clearResult();error();};
async function prepare(file){
 if(busy)return;removeImage();error();clearResult();
 if(!file||!['image/png','image/jpeg','image/webp'].includes(file.type)){error('请选择 JPEG、PNG 或 WebP 图片。');return;}
 if(file.size>5*1024*1024){error('图片超过 5MB，请缩小后重试。');return;}
 const version=++generation;preparing=true;refresh();let rawURL='';
 try{
  rawURL=URL.createObjectURL(file);const image=new Image();image.src=rawURL;await image.decode();
  if(image.naturalWidth*image.naturalHeight>40000000)throw new Error('图片像素过大，请缩小后重试。');
  const scale=Math.min(1,2400/Math.max(image.naturalWidth,image.naturalHeight));const canvas=document.createElement('canvas');canvas.width=Math.max(1,Math.round(image.naturalWidth*scale));canvas.height=Math.max(1,Math.round(image.naturalHeight*scale));const ctx=canvas.getContext('2d');ctx.fillStyle='#fff';ctx.fillRect(0,0,canvas.width,canvas.height);ctx.drawImage(image,0,0,canvas.width,canvas.height);
  const blob=await new Promise(resolve=>canvas.toBlob(resolve,'image/jpeg',.92));if(!blob||blob.size>5*1024*1024)throw new Error('图片处理后仍超过 5MB，请缩小后重试。');
  if(version!==generation)return;
  selected=blob;previewURL=URL.createObjectURL(blob);$('imagePreview').src=previewURL;$('previewBox').hidden=false;$('imageInfo').textContent=`已准备 ${canvas.width} × ${canvas.height} · ${Math.ceil(blob.size/1024)} KB`;
 }catch(e){if(version===generation)error(e.message||'无法读取图片，请重新选择。');}finally{if(rawURL)URL.revokeObjectURL(rawURL);if(version===generation){preparing=false;refresh();}}
}
$('imageFile').onchange=()=>{if($('imageFile').files.length!==1){error('一次请选择一张图片。');return;}prepare($('imageFile').files[0]);};
document.addEventListener('paste',e=>{if(busy)return;const file=[...(e.clipboardData?.items||[])].find(item=>item.type.startsWith('image/'))?.getAsFile();if(file){e.preventDefault();if(mode!=='image')setMode('image');prepare(file);}});
for(const name of ['dragover','dragleave','drop'])$('dropzone').addEventListener(name,e=>{e.preventDefault();$('dropzone').classList.toggle('dragging',name==='dragover');if(name==='drop'&&!busy){if(e.dataTransfer.files.length!==1){error('一次请拖入一张图片。');return;}prepare(e.dataTransfer.files[0]);}});
$('dropzone').onkeydown=e=>{if((e.key==='Enter'||e.key===' ')&&e.target===$('dropzone')){e.preventDefault();$('imageFile').click();}};
function zoom(src){if(!src)return;$('dialogImage').src=src;$('imageDialog').showModal();}
document.querySelectorAll('[data-image]').forEach(button=>button.onclick=()=>zoom(button.dataset.image));
$('previewZoom').onclick=()=>zoom(previewURL);$('closeDialog').onclick=()=>$('imageDialog').close();$('imageDialog').onclick=e=>{if(e.target===$('imageDialog'))$('imageDialog').close();};$('imageDialog').addEventListener('close',()=>$('dialogImage').removeAttribute('src'));
async function status(){
 try{const r=await fetch(API+'/status',{cache:'no-store',signal:AbortSignal.timeout(10000)});if(!r.ok)throw new Error();const s=await r.json();available=!!s.configured;remaining=s.remaining;model=s.model;imageSupported=s.imageSupported!==false;$('modelStatus').textContent=`模型：${model} · 今日上限 ${s.limit} 次（作文 / 数学共用） · ${available?'支持文字和图片点评':'服务暂未就绪'}`;if(!available)error('模型暂未就绪，两个预置示例仍可查看。');}
 catch(_){available=false;$('modelStatus').textContent='模型服务暂时连接不上，示例仍可查看。';error('请稍后刷新页面重试。');}refresh();
}
$('reviewForm').onsubmit=async e=>{
 e.preventDefault();refresh();if($('submitBtn').disabled)return;
 const data=new FormData();data.append('mode',mode);data.append('consent','yes');if(mode==='text')data.append('text',$('essayText').value.trim());else data.append('image',selected,'essay.jpg');
 busy=true;error();clearResult();$('resultTitle').textContent='正在阅读作文…';$('reviewText').textContent='正在生成一个优点和一个建议，请稍候。';refresh();
 const controller=new AbortController();const timer=setTimeout(()=>controller.abort(),118000);
 try{
  const r=await fetch(API,{method:'POST',body:data,signal:controller.signal});let payload;try{payload=await r.json();}catch(_){throw new Error('服务返回异常，请稍后重试。');}
  if(!r.ok){if(payload.code==='DAILY_LIMIT')remaining=0;if(payload.code==='IMAGE_UNSUPPORTED')imageSupported=false;throw new Error(payload.error||'生成失败，请稍后再试。');}
  output=payload.review;remaining=payload.remaining;$('resultBox').classList.add('ready');$('resultTitle').textContent='这一次的作文点评';$('resultMeta').textContent='本次实时生成 · '+payload.model;$('reviewText').textContent=output;$('reviewCount').textContent=`${payload.characters} / 100 字（含标点）`;$('copyResult').hidden=false;$('resultBox').scrollIntoView({block:'nearest',behavior:matchMedia('(prefers-reduced-motion: reduce)').matches?'instant':'smooth'});
 }catch(err){clearResult();error(err.name==='AbortError'?'分析超时，请稍后重试。':err.message);$('error').scrollIntoView({block:'nearest'});}
 finally{clearTimeout(timer);busy=false;refresh();}
};
refresh();status();
})();
