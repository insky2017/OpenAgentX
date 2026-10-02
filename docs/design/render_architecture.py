#!/usr/bin/env python3
"""Render the offline architecture atlas and Markdown from architecture-map.json.

Run from anywhere: python3 docs/design/render_architecture.py
Optional reading copy: --reading-dir /absolute/directory
"""
import argparse
import html
import json
import os
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[1]
STATUS = {'done': '已实现', 'partial': '部分实现 / 待补', 'planned': '待实现',
          'optional': '可选能力 / 未启用', 'external': '外部依赖'}

TEMPLATE = r'''<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light">
<title>OpenAgentX · 架构与关键流程</title>
<style>
:root{--ink:#183138;--muted:#577078;--line:#d4e1df;--paper:#f4f7f5;--card:#fff;--green:#126e56;--amber:#97560b;--purple:#735392;--blue:#356982;--grey:#657178;--accent:#147866;font-family:system-ui,-apple-system,"Noto Sans CJK SC","Microsoft YaHei",sans-serif;color:var(--ink);background:var(--paper)}
*{box-sizing:border-box}body{margin:0}button,a{-webkit-tap-highlight-color:transparent}button{font:inherit;cursor:pointer}a{color:var(--green);text-underline-offset:3px}button:focus-visible,a:focus-visible,summary:focus-visible{outline:3px solid #247aa4;outline-offset:4px}button{border:1px solid var(--line);background:white;color:var(--ink);border-radius:8px;padding:9px 13px}button:hover{border-color:var(--accent);background:#f1f8f5}.wrap{max-width:1392px;margin:auto;padding:0 30px}.masthead{display:flex;align-items:center;justify-content:space-between;padding-top:25px;gap:14px}.brand{font-size:13px;letter-spacing:.13em;font-weight:750;display:flex;align-items:center;gap:11px}.mark{background:var(--ink);color:white;border-radius:9px;padding:9px;letter-spacing:0}.edition{font-size:12px;color:var(--muted)}header h1{font-size:clamp(26px,3vw,37px);font-weight:700;margin:26px 0 9px;letter-spacing:-.03em}header p{color:var(--muted);font-size:14px;line-height:1.8;margin:0;max-width:950px}.meta{display:flex;gap:8px 18px;flex-wrap:wrap;margin-top:15px;font-size:12px;color:var(--muted)}code{font-family:ui-monospace,monospace;font-size:.95em;background:#edf2f0;padding:2px 5px;border-radius:4px;overflow-wrap:anywhere}.meta a{text-decoration:none}.tabs{display:grid;grid-template-columns:repeat(7,1fr);gap:8px;margin:25px 0 22px}.tabs button{text-align:left;padding:14px 12px;border-radius:9px;font-size:14px;font-weight:650;white-space:nowrap}.tabs button[aria-selected=true]{background:var(--ink);color:#fff;border-color:var(--ink);box-shadow:0 3px 6px #18313816}.tabs small{display:block;font-size:11px;font-weight:450;margin-top:5px;opacity:.72}.view-heading{display:flex;justify-content:space-between;gap:18px;align-items:flex-start}.eyebrow{color:var(--green);font-size:12px;font-weight:700;letter-spacing:.05em;margin-bottom:5px}h2{font-size:23px;margin:0 0 9px;letter-spacing:-.025em}.takeaway{font-size:14px;line-height:1.8;color:var(--muted);margin:0 0 16px}.controls{display:flex;flex-wrap:wrap;gap:7px;justify-content:flex-end;max-width:370px}.controls button{font-size:12px}.controls button[aria-pressed=true]{background:#fff0da;border-color:#dca35c;color:#7d4a11}.legend{display:flex;gap:10px 18px;flex-wrap:wrap;align-items:center;font-size:12px;margin-bottom:13px;color:var(--muted)}.legend i{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:6px}.legend span{white-space:nowrap}.legend .keyline{width:22px;height:0;border-top:2px solid #526e77;border-radius:0;vertical-align:middle}.legend .dash{border-top-style:dashed}.diagram-shell{background:#fff;border:1px solid var(--line);border-radius:14px;overflow:hidden;box-shadow:0 5px 20px #193d3610}.diagram-hint{padding:12px 18px;font-size:12px;color:var(--muted);border-bottom:1px solid #e7eeeb;display:flex;justify-content:space-between;gap:12px}.scroller{overflow-x:auto;overscroll-behavior-x:contain;scrollbar-width:thin}.scroller svg{display:block;width:100%;min-width:1050px;height:auto;background:radial-gradient(#cddbd84a .8px,transparent .8px);background-size:18px 18px}.scroller.zoom svg{width:145%;min-width:1450px}.row-label{fill:#6c8589;font-size:12px;font-weight:600;letter-spacing:.06em}.groupbox{fill:#eff6f3;stroke:#a8c6bd;stroke-dasharray:5 5}.group-label{fill:#527669;font-size:11px}.node{cursor:pointer;outline:none;transition:opacity .2s}.node .card{fill:white;stroke:#cadbd7;stroke-width:1.4;filter:drop-shadow(0 3px 3px #143b3210)}.node:hover .card,.node:focus .card,.node.selected .card{stroke:#197864;stroke-width:2.7}.node .status-dot{fill:var(--node-color)}.node .status-text{fill:var(--node-color);font-size:11px;font-weight:650}.node .node-title{font-size:17px;font-weight:680;fill:var(--ink)}.node .node-sub{font-size:12px;fill:#577078}.node.planned .card{stroke:#b5a0c6;stroke-dasharray:6 4;fill:#fcfaff}.node.optional .card{stroke:#a6bac6;stroke-dasharray:3 4;fill:#f9fbfd}.node.external .card{fill:#f3f6f5;stroke:#c6d0cf}.node.partial .card{fill:#fffaf1;stroke:#d7b786}.node .detail-icon{fill:#93aba5;font-size:13px}.gap-focus .node.done,.gap-focus .node.external{opacity:.3}.gap-focus .node.selected{opacity:1}.edge{fill:none;stroke:#718d91;stroke-width:1.6;stroke-linecap:round;stroke-linejoin:round}.edge.control{stroke:#5d8099;stroke-dasharray:5 4}.edge.plan{stroke:#9279a9;stroke-dasharray:3 5}.edge.optional{stroke:#63839a;stroke-dasharray:3 5}.edge-label{font-size:11px;fill:#577078;paint-order:stroke;stroke:#fff;stroke-width:5px;stroke-linejoin:round}.edge-label.plan{fill:#806198}.diagram-notes{display:grid;grid-template-columns:repeat(3,1fr);gap:18px;padding:17px 20px;background:#f7faf8;border-top:1px solid #e4ece8;color:#577078;font-size:12px;line-height:1.8}.diagram-notes p{margin:0}.diagram-notes b{color:#46776a;margin-right:6px}.details{margin-top:18px;background:white;border:1px solid var(--line);border-radius:12px;padding:22px;scroll-margin-top:16px}.details-top{display:flex;align-items:center;justify-content:space-between;gap:15px;margin-bottom:16px}.details h3{font-size:20px;margin:0}.badge{display:inline-block;font-size:11px;border:1px solid currentColor;border-radius:5px;padding:3px 7px;white-space:nowrap}.detail-grid{display:grid;grid-template-columns:1.2fr 1fr;gap:26px}.detail-grid h4{font-size:12px;font-weight:650;color:var(--muted);margin:0 0 7px}.detail-grid p{font-size:14px;line-height:1.9;margin:0 0 14px}.risk{border-left:3px solid #cc9c59;padding-left:12px;color:#815624}.refs{display:flex;flex-wrap:wrap;gap:7px 13px;margin:10px 0 0}.refs a{font-size:12px;line-height:1.7}.hint{font-size:12px!important;color:var(--muted)}.jump{font-size:12px;padding:7px 11px;margin-top:4px}.section{margin:26px 0}details.disclosure{background:#fff;border:1px solid var(--line);border-radius:12px;padding:17px 20px;margin-top:13px}summary{cursor:pointer;font-size:14px;font-weight:650}summary .muted{font-weight:400;color:var(--muted);margin-left:12px;font-size:12px}.table-wrap{overflow:auto;margin-top:17px}table{border-collapse:collapse;width:100%;min-width:850px;font-size:13px}th,td{padding:12px;text-align:left;vertical-align:top;border-bottom:1px solid #e4ece8;line-height:1.8}th{font-size:12px;color:var(--muted);background:#f7faf8}td:first-child{font-weight:650;min-width:165px}td button{font-size:12px;padding:5px 8px;margin-top:6px}.evidence-body{font-size:13px;line-height:1.9;margin-top:17px}.source-index{columns:2;column-gap:28px;padding-left:21px}.source-index li{break-inside:avoid;margin:6px 0}.source-index small{display:block;color:var(--muted);overflow-wrap:anywhere}.proof-grid{display:grid;grid-template-columns:repeat(3,1fr);gap:13px;margin:15px 0}.proof-grid div{border:1px solid #e3ebe7;background:#f7faf8;border-radius:8px;padding:12px}.proof-grid b{display:block;margin-bottom:4px}footer{font-size:12px;line-height:1.9;color:var(--muted);padding:6px 0 25px}footer a{margin-right:15px}.sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
@media(max-width:800px){.wrap{padding:0 16px}.masthead{padding-top:18px}.edition{font-size:10px}.brand{font-size:11px}.tabs{grid-template-columns:repeat(3,1fr);gap:6px;margin:20px 0}.tabs button{font-size:12px;padding:11px 9px}.tabs small{font-size:10px}.view-heading{display:block}.controls{justify-content:flex-start;max-width:none;margin:0 0 13px}h2{font-size:21px}.diagram-notes{grid-template-columns:1fr;gap:8px;padding:13px}.diagram-hint{font-size:11px;padding:10px}.detail-grid{grid-template-columns:1fr;gap:6px}.details{padding:16px}.details h3{font-size:18px}.details-top{align-items:flex-start}.source-index{columns:1}.proof-grid{grid-template-columns:1fr}.meta{font-size:11px}summary .muted{display:block;margin:7px 0 0}.scroller svg{min-width:1050px}.section{margin:20px 0}.legend{gap:8px 12px;font-size:11px}}
@media print{.wrap{max-width:none;padding:0}.controls,.tabs,footer,.diagram-hint{display:none}.scroller{overflow:visible}.scroller svg{min-width:0!important;width:100%!important}.diagram-shell,.details{box-shadow:none;break-inside:avoid}header h1{font-size:23px}.details{margin-top:10px}.section{display:none}body{background:white}}
</style>
</head>
<body>
<div class="wrap">
<header>
 <div class="masthead"><div class="brand"><span class="mark">OAX</span> OPENAGENTX / SYSTEM ATLAS</div><span class="edition">实现快照 · @@DATE@@ · 离线可用</span></div>
 <h1>架构与关键流程</h1>
 <p>先看全局，再进入关键链路。每个节点都附有职责、实现边界与源码／验收依据。范围是 OpenAgentX 任务后台；交易业务作为受管工作，不在此展开。</p>
 <div class="meta"><span>核对源码 <code>@@SOURCE@@</code></span><span>安装工件来源 <code>@@INSTALLED@@</code></span><span>本机 Linux · AGY / Codex 已装配并安装</span><a href="@@MARKDOWN@@">阅读 Markdown ↗</a></div>
</header>
<nav class="tabs" role="tablist" aria-label="架构视图" id="tabs"></nav>
<main id="panel" role="tabpanel" tabindex="-1">
 <div class="view-heading"><div><div class="eyebrow" id="question"></div><h2 id="view-title"></h2><p class="takeaway" id="takeaway"></p></div><div class="controls"><button id="gaps-toggle" aria-pressed="false">突出待补能力</button><button id="zoom-toggle" aria-pressed="false">放大图面</button><button id="export-svg">下载当前 SVG</button></div></div>
 <div class="legend" aria-label="实现状态图例"><span><i style="background:#126e56"></i>已实现</span><span><i style="background:#97560b"></i>部分实现 / 待补</span><span><i style="background:#735392"></i>待实现</span><span><i style="background:#356982"></i>可选 / 未启用</span><span><i style="background:#657178"></i>外部依赖</span><span><i class="keyline"></i>执行 / 数据</span><span><i class="keyline dash"></i>控制 / 拟议（见标注）</span></div>
 <div class="diagram-shell"><div class="diagram-hint"><span>点击节点选择 · 详情在图下</span><button id="detail-open" style="padding:0;border:0;font-size:12px;color:#147866">查看所选节点详情 ↓</button></div><div id="scroller" class="scroller" tabindex="0" role="region" aria-label="架构图，可横向滚动"><svg id="diagram" xmlns="http://www.w3.org/2000/svg" role="group"></svg></div><div id="notes" class="diagram-notes"></div></div>
 <section class="details" id="details" aria-label="节点详情"><div class="details-top"><h3 id="detail-title"></h3><span id="detail-status" class="badge"></span></div><div class="detail-grid"><div><h4>职责与行为</h4><p id="detail-text"></p><p id="detail-risk" class="risk"></p><button id="detail-jump" class="jump" hidden></button></div><div><h4>证据范围</h4><p id="detail-proof"></p><p class="hint">D = 确定性测试；R = 真实 Runtime；I = 已安装环境。标为“已实现”不代表全部边界场景均已真实验收。</p><h4>进一步核对</h4><div id="detail-refs" class="refs"></div></div></div></section>
 <div class="sr-only" id="announce" aria-live="polite"></div>
</main>
<section class="section" aria-label="边界与证据">
 <details class="disclosure" id="gaps"><summary>待补能力与建议调整点 <span class="muted">先处理日用阻力，再扩展平台能力</span></summary><div class="table-wrap"><table><thead><tr><th>能力 / 状态</th><th>当前事实</th><th>最小下一步</th><th>何时需要处理</th></tr></thead><tbody id="gap-rows"></tbody></table></div></details>
 <details class="disclosure"><summary>版本、证据与源码索引 <span class="muted">结论可追溯；历史证据不等于本版全量重测</span></summary><div class="evidence-body"><p>源码核对基线 <code>@@SOURCE@@</code>；当前已安装 Go / Web 工件来源为 <code>@@INSTALLED@@</code>。本图是文档快照，不实时查询服务状态。实际安装二进制 SHA-256：</p><p><code>@@SHA@@</code></p><div class="proof-grid"><div><b>D · 确定性</b>事务、状态规则、竞争和故障注入；不能代替模型或真实进程验证。</div><div><b>R · 真实 Runtime</b>AGY / Codex 分别以实际日志、Task / Run / Journal、文件或进程效果交叉核验。</div><div><b>I · 安装环境</b>核实已安装工件与运行来源，再通过真实 Web / PTY 完成用户流程。</div></div><p>AGY 历史验收保留；Codex 已装配并安装，进程来源、Web静态工件和网络重启持久性 I 已通过。API、迁入、native交互及其他用例按覆盖记录分别判定；精确取消和异常恢复仍有缺口。源码、R与I分开，原始失败和复验分别保存。</p><ol class="source-index" id="source-index"></ol></div></details>
</section>
<footer><p>维护方式：编辑 <code>docs/design/architecture-map.json</code>，运行 <code>python3 docs/design/render_architecture.py</code>，同步生成 HTML 与 Markdown。服务、协议、状态或验收结论变化时，更新对应节点及依据。</p><a href="@@MARKDOWN@@">Markdown 说明</a><span>不加载外部脚本、字体或图表服务。来源链接指向本地源码与证据文件。</span></footer>
</div>
<script id="architecture-data" type="application/json">@@DATA@@</script>
<script>
'use strict';
const data=JSON.parse(document.getElementById('architecture-data').textContent);
const statusNames={done:'已实现',partial:'部分实现 / 待补',planned:'待实现',optional:'可选 / 未启用',external:'外部依赖'};
const colors={done:'#126e56',partial:'#97560b',planned:'#735392',optional:'#356982',external:'#657178'};
const descriptions=['结构与职责','提交到验收','领域与交接','长运行与故障','事件与重连','部署与边界'];
const $=id=>document.getElementById(id),NS='http://www.w3.org/2000/svg';
function s(tag,attrs={},content=''){const el=document.createElementNS(NS,tag);Object.entries(attrs).forEach(([k,v])=>el.setAttribute(k,v));if(content)el.textContent=content;return el;}
function e(tag,cls,text){const el=document.createElement(tag);if(cls)el.className=cls;if(text)el.textContent=text;return el;}
let current,selected;
function route(edge,nodes){if(edge.points)return edge.points;const a=nodes.get(edge.a),b=nodes.get(edge.b);if(a.y===b.y){const right=b.x>a.x;return [[a.x+(right?a.w:0),a.y+a.h/2],[b.x+(right?0:b.w),b.y+b.h/2]];}if(a.x===b.x){const down=b.y>a.y;return [[a.x+a.w/2,a.y+(down?a.h:0)],[b.x+b.w/2,b.y+(down?0:b.h)]];}const ax=a.x+a.w/2,bx=b.x+b.w/2,ay=a.y+a.h,by=b.y;return [[ax,ay],[ax,(ay+by)/2],[bx,(ay+by)/2],[bx,by]];}
function labelPoint(points){let best=0,len=-1;for(let i=0;i<points.length-1;i++){const p=points[i],q=points[i+1],l=Math.abs(p[0]-q[0])+Math.abs(p[1]-q[1]);if(l>len){best=i;len=l;}}const p=points[best],q=points[best+1];const vertical=p[0]===q[0];return {x:(p[0]+q[0])/2+(vertical?9:0),y:(p[1]+q[1])/2+(vertical?4:-9),anchor:vertical?'start':'middle'};}
function showDetail(id,announce=false){const n=current.nodes.find(x=>x.id===id);if(!n)return;selected=id;document.querySelectorAll('.node').forEach(el=>{const active=el.dataset.id===id;el.classList.toggle('selected',active);el.setAttribute('aria-pressed',String(active));});$('detail-title').textContent=n.title;$('detail-open').textContent=n.title+' · 详情 ↓';$('detail-status').textContent=statusNames[n.status];$('detail-status').style.color=colors[n.status];$('detail-text').textContent=n.detail;$('detail-proof').textContent=n.proof;$('detail-risk').textContent=n.risk;$('detail-risk').hidden=!n.risk;const jump=data.views.find(x=>x.id===n.jump);$('detail-jump').hidden=!jump;if(jump){$('detail-jump').textContent='展开 '+jump.tag+' →';$('detail-jump').onclick=()=>{setView(jump.id);$('view-title').scrollIntoView({block:'start',behavior:'smooth'});};} $('detail-refs').replaceChildren(...n.refs.map(k=>{const r=data.refs[k],a=e('a','',r.label+' ↗');a.href=r.href;a.target='_blank';a.rel='noopener';a.title=r.path+':'+r.line;return a;}));if(announce)$('announce').textContent='已选择 '+n.title+'，下方节点详情已更新。';}
function setView(id){current=data.views.find(v=>v.id===id)||data.views[0];history.replaceState(null,'','#'+current.id);document.querySelectorAll('[role=tab]').forEach(b=>{const active=b.dataset.view===current.id;b.setAttribute('aria-selected',String(active));b.tabIndex=active?0:-1;});$('panel').setAttribute('aria-labelledby','tab-'+current.id);$('question').textContent=current.question;$('view-title').textContent=current.title;$('takeaway').textContent=current.takeaway;const svg=$('diagram');svg.replaceChildren();svg.setAttribute('viewBox',`0 0 ${current.width} ${current.height}`);svg.setAttribute('aria-labelledby','svg-title svg-desc');svg.append(s('title',{id:'svg-title'},current.title),s('desc',{id:'svg-desc'},current.takeaway+' 点击节点阅读实现状态和证据。'));
const defs=s('defs');for(const [kind,color]of Object.entries({data:'#718d91',control:'#5d8099',plan:'#9279a9',optional:'#63839a'})){const m=s('marker',{id:'arrow-'+kind,viewBox:'0 0 10 10',refX:9,refY:5,markerWidth:6,markerHeight:6,orient:'auto-start-reverse'});m.append(s('path',{d:'M 0 0 L 10 5 L 0 10 z',fill:color}));defs.append(m);}svg.append(defs);
for(const g of current.groups||[]){svg.append(s('rect',{class:'groupbox',x:g.x,y:g.y,width:g.w,height:g.h,rx:12}),s('text',{class:'group-label',x:g.x+g.w/2,y:g.ly,'text-anchor':'middle'},g.label));}
for(const [y,text]of current.rows)svg.append(s('text',{class:'row-label',x:30,y},text));const nodes=new Map(current.nodes.map(n=>[n.id,n]));
for(const edge of current.edges){const p=route(edge,nodes),label=edge.label_pos||labelPoint(p);svg.append(s('path',{class:'edge '+edge.kind,d:p.map((x,i)=>(i?'L':'M')+x.join(' ')).join(' '),'marker-end':'url(#arrow-'+edge.kind+')'}));svg.append(s('text',{class:'edge-label '+edge.kind,x:label.x,y:label.y,'text-anchor':label.anchor},edge.label));}
for(const n of current.nodes){const g=s('g',{class:'node '+n.status,transform:`translate(${n.x} ${n.y})`,role:'button',tabindex:0,'aria-label':n.title+'，'+statusNames[n.status]+'，查看详情','aria-pressed':'false','data-id':n.id,style:'--node-color:'+colors[n.status]});g.append(s('title',{},n.title+'：'+n.sub),s('rect',{class:'card',x:0,y:0,width:n.w,height:n.h,rx:11}),s('circle',{class:'status-dot',cx:17,cy:21,r:3}),s('text',{class:'status-text',x:26,y:25},statusNames[n.status]),s('text',{class:'detail-icon',x:n.w-23,y:25},'↗'),s('text',{class:'node-title',x:16,y:56},n.title),s('text',{class:'node-sub',x:16,y:82},n.sub));g.addEventListener('click',()=>showDetail(n.id,true));g.addEventListener('keydown',ev=>{if(ev.key==='Enter'||ev.key===' '){ev.preventDefault();showDetail(n.id,true);}});svg.append(g);}
$('notes').replaceChildren(...current.notes.map((note,i)=>{const p=e('p');p.append(e('b','',String(i+1).padStart(2,'0')),document.createTextNode(note));return p;}));$('scroller').scrollLeft=0;showDetail(current.nodes.find(n=>n.status==='partial'||n.status==='planned')?.id||current.nodes[0].id);}
data.views.forEach((v,i)=>{const b=e('button','',v.tag);b.id='tab-'+v.id;b.dataset.view=v.id;b.setAttribute('role','tab');b.setAttribute('aria-controls','panel');b.append(e('small','',v.description||descriptions[i]));b.onclick=()=>setView(v.id);b.onkeydown=ev=>{let next;if(ev.key==='ArrowRight')next=(i+1)%data.views.length;else if(ev.key==='ArrowLeft')next=(i+data.views.length-1)%data.views.length;else if(ev.key==='Home')next=0;else if(ev.key==='End')next=data.views.length-1;if(next!==undefined){ev.preventDefault();setView(data.views[next].id);$('tab-'+data.views[next].id).focus();}};$('tabs').append(b);});
$('detail-open').onclick=()=>$('details').scrollIntoView({block:'start',behavior:'smooth'});
$('gaps-toggle').onclick=()=>{const active=$('gaps-toggle').getAttribute('aria-pressed')!=='true';$('gaps-toggle').setAttribute('aria-pressed',String(active));$('diagram').classList.toggle('gap-focus',active);};$('zoom-toggle').onclick=()=>{const active=$('zoom-toggle').getAttribute('aria-pressed')!=='true';$('zoom-toggle').setAttribute('aria-pressed',String(active));$('zoom-toggle').textContent=active?'恢复图面':'放大图面';$('scroller').classList.toggle('zoom',active);};
$('export-svg').onclick=()=>{const svg=$('diagram').cloneNode(true);svg.classList.remove('gap-focus');svg.setAttribute('width',current.width);svg.setAttribute('height',current.height);svg.setAttribute('style','--ink:#183138;font-family:system-ui,sans-serif;background:white');const style=s('style');style.textContent=document.querySelector('style').textContent;svg.insertBefore(style,svg.firstChild);svg.querySelectorAll('.node').forEach(g=>g.classList.remove('selected'));const blob=new Blob([new XMLSerializer().serializeToString(svg)],{type:'image/svg+xml;charset=utf-8'}),url=URL.createObjectURL(blob),a=e('a');a.href=url;a.download='openagentx-'+current.id+'-'+data.date+'.svg';document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);};
for(const gap of data.gaps){const tr=e('tr'),title=e('td');title.append(e('div','',gap.name));const badge=e('span','badge',statusNames[gap.status]);badge.style.color=colors[gap.status];title.append(badge,e('br'));const b=e('button','','查看关联子图 →');b.onclick=()=>{setView(gap.view);$('view-title').scrollIntoView({behavior:'smooth'});};title.append(b);tr.append(title,...[gap.current,gap.next,gap.trigger].map(text=>e('td','',text)));$('gap-rows').append(tr);}
for(const r of Object.values(data.refs)){const li=e('li'),a=e('a','',r.label+' ↗');a.href=r.href;a.target='_blank';a.rel='noopener';li.append(a,e('small','',r.path+':'+r.line));$('source-index').append(li);}
window.addEventListener('hashchange',()=>setView(location.hash.slice(1)));setView(location.hash.slice(1)||'overview');
</script>
</body></html>
'''


def validate(data):
    ids = {v['id'] for v in data['views']}
    for ref in data['refs'].values():
        p = ROOT / ref['path']
        assert p.is_file(), p
        assert 1 <= ref['line'] <= len(p.read_text().splitlines()), ref
    for v in data['views']:
        nodes = {n['id'] for n in v['nodes']}
        assert len(nodes) == len(v['nodes'])
        for n in v['nodes']:
            assert n['status'] in STATUS
            assert not n['jump'] or n['jump'] in ids
            assert all(r in data['refs'] for r in n['refs'])
        for edge in v['edges']:
            assert edge['a'] in nodes and edge['b'] in nodes
            assert edge['kind'] in ('data', 'control', 'plan', 'optional')
    assert all(g['view'] in ids for g in data['gaps'])


def markdown(data, destination, html_name, absolute=False):
    def link(k):
        r = data['refs'][k]
        path = str(ROOT / r['path']) if absolute else os.path.relpath(ROOT / r['path'], destination)
        return f"[{r['label']}]({path})"
    lines = [f"# {data['title']}\n", f"交互图：[打开 HTML 图册]({html_name})。快照日期：{data['date']}。",
        "本图描述 OpenAgentX 任务后台。交易、行情等属于受管领域及业务工作区；不声称券商、交易所或真实交易执行已经接通。\n",
        "## 要点\n",
        "- 控制面是一个 daemon；每个领域有独立 Worker service。API、领域服务不是分别部署的微服务。",
        "- SQLite 保存 Task、Run、投递与审计等权威事实；内存 Broker 只用于唤醒。Worker 通过正式 API 工作，不直接写库。",
        "- 每次 Run 冻结角色、工作目录、输入与期限。AGY 每轮启动 CLI；Codex 由常驻 app-server 管理 thread/turn。原生 TUI 的写入经桥接进入正式调度，关闭界面不会终止 Worker。",
        "- 问答成功表示完整回复已交付；执行任务的业务效果需独立核验。人工接受是独立 review，不改写原始执行事实。",
        "- AGY 保留历史验收；Codex 已装配并安装，来源、Web静态工件及网络重启 I 已通过。任务、迁入和原生交互 I 按覆盖表分别验收；精确取消和异常恢复仍待补。\n",
        "## 当前版本与阅读方法\n",
        f"核对源码：`{data['source_commit']}`；当前安装工件来源：`{data['installed_commit']}`。本图不是服务实时监控。后续文档提交不会自动改变安装来源。",
        f"\n实际安装二进制 SHA-256：`{data['binary_sha256']}`。",
        f"\n证据入口：{link('installed')}、{link('daily')}、{link('scope')}；Codex 本轮见 {link('codexcoverage')}、{link('codexdelivery')}、{link('codexlive')}、{link('codexinstalled')}。AGY 历史证据按影响复用；Codex 安装来源/环境 I 不代表全部用户流程通过。",
        "\n实现状态与证据强度是两条轴：**已实现**、**部分实现 / 待补**、**待实现**、**可选 / 未启用**、**外部依赖**；证据标注 **D**（确定性）、**R**（真实 Runtime）、**I**（已安装环境）。绿色不等于全部 E2E 通过。",
        "\nHTML 每次只显示一个子图；点击节点可见职责、限制和引用。虚线区分控制与拟议关系，以箭头文字和节点状态为准。\n"]
    for v in data['views']:
        lines += [f"## {v['tag']}：{v['title']}\n", f"**{v['question']}** {v['takeaway']}\n",
                  f"[打开此图]({html_name}#{v['id']})\n", "```mermaid", "flowchart LR"]
        for n in v['nodes']:
            lines.append(f'  {n["id"]}["{n["title"]}<br/>{STATUS[n["status"]]}"]')
        for edge in v['edges']:
            arrow = '-->' if edge['kind']=='data' else '-.->'
            lines.append(f'  {edge["a"]} {arrow}|"{edge["label"]}"| {edge["b"]}')
        lines += ['```\n', "| 节点 | 状态 | 关键职责 / 限制 | 证据范围 |", "|---|---|---|---|"]
        for n in v['nodes']:
            text = n['detail'] + (' **待补：'+n['risk']+'**' if n['risk'] else '')
            refs = '、'.join(link(k) for k in n['refs'])
            lines.append(f"| {n['title']} | {STATUS[n['status']]} | {text.replace('|','／')} | {n['proof']}。{refs} |")
        lines += [''] + [f"- {note}" for note in v['notes']] + ['']
    lines += ['## 调整优先级与未完成能力\n', '| 能力 | 当前事实 | 建议最小调整 | 重新处理的触发条件 |', '|---|---|---|---|']
    for g in data['gaps']:
        lines.append(f"| {g['name']}（{STATUS[g['status']]}） | {g['current']} | {g['next']} | {g['trigger']} |")
    lines += ['\n## 源码与证据索引\n']
    for k,r in data['refs'].items():
        lines.append(f"- {link(k)}：`{r['path']}:{r['line']}`。")
    lines += ['\n## 下一步与维护\n',
        '1. 先读总览，再进入工作流、初始化、运行恢复、观察、依赖或 Codex 原生子图；按每个节点的 D/R/I 证据阅读。',
        '2. Codex 已安装，继续按覆盖表收口用户流程；精确取消和异常恢复保留缺口。自有host全树兜底可能影响旧后台工具，普通resume不解除未决隔离。',
        '3. 编辑 `docs/design/architecture-map.json` 后运行 `python3 docs/design/render_architecture.py`，同时更新两份输出；需要同步阅读入口时追加 `--reading-dir /home/sky/Documents/ChatGPT/OpenAgentX`。更新节点时同步来源、证据版本与限制，避免架构文档再次落后实现。',
        '\n本图记录本轮源码及证据快照，不代表实时服务状态；原架构与失败证据由 Git 及对应批次目录保留。\n']
    return '\n'.join(lines)


def render(data, destination, html_name, md_name, absolute=False):
    destination.mkdir(parents=True, exist_ok=True)
    view_data = json.loads(json.dumps(data))
    for r in view_data['refs'].values():
        target = ROOT / r['path']
        r['href'] = target.as_uri() if absolute else os.path.relpath(target, destination)
    embedded = json.dumps(view_data, ensure_ascii=False).replace('<', '\\u003c')
    substitutions = {'DATE': data['date'], 'SOURCE': data['source_commit'],
                     'INSTALLED': data['installed_commit'][:7], 'SHA': data['binary_sha256'],
                     'MARKDOWN': md_name}
    output = TEMPLATE
    for k,value in substitutions.items():
        output = output.replace('@@'+k+'@@',html.escape(value,quote=True))
    output = output.replace('@@DATA@@',embedded)
    (destination / html_name).write_text(output)
    (destination / md_name).write_text(markdown(data,destination,html_name,absolute))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--reading-dir',type=Path)
    args=parser.parse_args()
    data=json.loads((HERE / 'architecture-map.json').read_text())
    validate(data)
    render(data,HERE,'current-architecture.html','CURRENT_ARCHITECTURE.md')
    if args.reading_dir:
        render(data,args.reading_dir,'OpenAgentX-架构与关键流程.html','OpenAgentX-架构与关键流程.md',True)
    print(f"Rendered {len(data['views'])} views, {sum(len(v['nodes']) for v in data['views'])} nodes; all source references checked.")


if __name__ == '__main__':
    main()
