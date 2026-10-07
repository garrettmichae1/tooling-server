const $=id=>document.getElementById(id);
function el(tag,text){const n=document.createElement(tag);if(text!==undefined)n.textContent=String(text);return n}
function svgEl(tag,attrs,text){const n=document.createElementNS('http://www.w3.org/2000/svg',tag);for(const [k,v]of Object.entries(attrs))n.setAttribute(k,String(v));if(text!==undefined)n.textContent=String(text);return n}
function download(text,name,type){const url=URL.createObjectURL(new Blob([text],{type}));const a=el('a');a.href=url;a.download=name;document.body.append(a);a.click();a.remove();setTimeout(()=>URL.revokeObjectURL(url),1000)}
function csvCell(value,isText){let s=String(value);if(isText&&/^[\s]*[=+\-@]/.test(s))s="'"+s;return '"'+s.replaceAll('"','""')+'"'}
function numberText(n){return Number(n).toLocaleString('en-US',{maximumSignificantDigits:8})}
function axisText(n){return Math.abs(n)>=1e6||(n!==0&&Math.abs(n)<.001)?n.toExponential(2):numberText(n)}
const COLORS=['#2563eb','#b91c1c','#047857','#7c3aed','#b45309','#0e7490'];
function plot(svg,labels,series,type,active=[]){
 svg.replaceChildren();if(!series.length)return;
 const W=Math.max(280,Math.min(800,Math.round(svg.getBoundingClientRect().width))),H=W<500?300:360,L=76,R=18,T=20,B=55,innerW=W-L-R,innerH=H-T-B;
 svg.setAttribute('viewBox','0 0 '+W+' '+H);
 let low=0,high=0;for(const s of series)for(const v of s.values){low=Math.min(low,v);high=Math.max(high,v)}if(high===low){high=low+1}
 const y=v=>T+(high-v)/(high-low)*innerH;
 for(let i=0;i<=4;i++){const v=low+(high-low)*i/4;svg.append(svgEl('line',{x1:L,x2:W-R,y1:y(v),y2:y(v),stroke:'#d9e1ec'}),svgEl('text',{x:L-8,y:y(v)+4,'text-anchor':'end',fill:'#526074','font-size':12},axisText(v)))}
 svg.append(svgEl('line',{x1:L,x2:W-R,y1:y(0),y2:y(0),stroke:'#526074'}));
 const slot=innerW/labels.length,center=i=>L+slot*(i+.5);
 series.forEach((s,j)=>{
  const color=s.color||COLORS[j%COLORS.length];
  if(type==='line'){svg.append(svgEl('polyline',{points:s.values.map((v,i)=>center(i)+','+y(v)).join(' '),fill:'none',stroke:color,'stroke-width':3}));s.values.forEach((v,i)=>{const c=svgEl('circle',{cx:center(i),cy:y(v),r:4,fill:color});c.append(svgEl('title',{},labels[i]+': '+String(v)));svg.append(c)})}
  else{s.values.forEach((v,i)=>{const width=slot*.8/series.length,x=L+slot*i+slot*.1+j*width;const rect=svgEl('rect',{x,y:Math.min(y(v),y(0)),width:Math.max(.2,width-1),height:Math.abs(y(v)-y(0)),fill:active.includes(i)?'#b45309':color});rect.append(svgEl('title',{},labels[i]+': '+String(v)));svg.append(rect)})}
 });
 const every=Math.max(1,Math.ceil(labels.length/(W<500?4:8)));let lastRight=-Infinity;
 labels.forEach((label,i)=>{if(i%every!==0&&i!==labels.length-1)return;const text=String(label),limit=W<500?9:12;const tick=svgEl('text',{x:center(i),y:H-25,'text-anchor':'middle',fill:'#526074','font-size':12},text.length>limit?text.slice(0,limit-1)+'…':text);tick.append(svgEl('title',{},text));svg.append(tick);const box=tick.getBBox();if(box.x<lastRight+12||box.x+box.width>W-6)tick.remove();else lastRight=box.x+box.width});
}
