() => {
const out=[];
for(const v of data.views){
 $('tab-'+v.id).click();
 const textOverflow=[...document.querySelectorAll('.node-title,.node-sub,.status-text')].filter(t=>t.getBBox().x+t.getBBox().width>current.nodes.find(n=>n.id===t.closest('.node').dataset.id).w-10).map(t=>t.textContent);
 const labelOverlaps=[];
 for(const t of document.querySelectorAll('.edge-label,.row-label')){
 const b=t.getBBox();
 for(const n of current.nodes)if(b.x<n.x+n.w&&b.x+b.width>n.x&&b.y<n.y+n.h&&b.y+b.height>n.y)labelOverlaps.push({label:t.textContent,node:n.id});
 }
 const lineOverlaps=[];
 for(const edge of current.edges){const p=route(edge,new Map(current.nodes.map(n=>[n.id,n])));
 for(const n of current.nodes){if([edge.a,edge.b].includes(n.id))continue;
 for(let i=0;i<p.length-1;i++){const [x1,y1]=p[i],[x2,y2]=p[i+1];
 const hit=x1===x2?x1>n.x&&x1<n.x+n.w&&Math.max(y1,y2)>n.y&&Math.min(y1,y2)<n.y+n.h:y1>n.y&&y1<n.y+n.h&&Math.max(x1,x2)>n.x&&Math.min(x1,x2)<n.x+n.w;
 if(hit)lineOverlaps.push({edge:edge.label,node:n.id});}}}
 let allDetails=true;
 for(const n of v.nodes){document.querySelector('.node[data-id="'+n.id+'"]').dispatchEvent(new MouseEvent('click',{bubbles:true}));allDetails=allDetails&&$('detail-title').textContent===n.title&&$('detail-status').textContent===statusNames[n.status];}
 out.push({view:v.id,nodes:current.nodes.length,textOverflow,labelOverlaps,lineOverlaps,allDetails});
}
setView('overview');return {checkedAt:new Date().toISOString(),userAgent:navigator.userAgent,views:out,externalResources:performance.getEntriesByType('resource').map(r=>r.name),width:innerWidth,pageWidth:document.documentElement.scrollWidth};
}
