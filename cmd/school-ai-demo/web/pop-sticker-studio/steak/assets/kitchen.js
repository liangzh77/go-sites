// Geometry shared by the web stage and the frame exporter. All points are in screen pixels.
function kitchenFrame(t,W,H,tl){
 const clamp=v=>Math.max(0,Math.min(1,v)),ease=v=>{v=clamp(v);return v*v*(3-2*v)},r=(a,b)=>ease((t-a)/(b-a)),mix=(a,b,u)=>a+(b-a)*u;
 const aspects={pan:554/745,raw:410/746,seared:497/744,butter:451/467,spoon:546/739,plate:501/789,oil:220/300};
 const q=t<=3?t:t<=3.54?3:3+(t-3.54)/.46;const clock=t>=4?t:q;
 const basis=Math.min(W,H*1.6),{times,poses}=tl;let i=0;while(i<times.length-2&&clock>times[i+1])i++;const u=ease((clock-times[i])/(times[i+1]-times[i]));const sprites={};
 for(const [k,steps] of Object.entries(poses)){const [x,y,w,a,o]=steps[i].map((n,j)=>mix(n,steps[i+1][j],u));sprites[k]={x:x*W/100,y:y*H/100,w:w*basis/100,h:w*basis*aspects[k]/100,a,o}}
 function point(s,fx,fy){const dx=(fx-.5)*s.w,dy=(fy-.5)*s.h,a=s.a*Math.PI/180;return {x:s.x+dx*Math.cos(a)-dy*Math.sin(a),y:s.y+dx*Math.sin(a)+dy*Math.cos(a)}}
 function pin(s,fx,fy,target){let p=point(s,fx,fy);s.x+=target.x-p.x;s.y+=target.y-p.y}
 const {pan,seared,oil,spoon,butter}=sprites;const pocket=point(pan,.78,.68),hit=point(seared,.5,.43),oilHit=point(pan,.53,.64);
 // The bottle tips into position BEFORE the oil starts and only leaves once the stream stops.
 if(t>=1.34&&t<=1.85){const settled=r(1.34,1.49)*(1-r(1.75,1.85));oil.o=settled;oil.a=mix(-12,0,settled);pin(oil,.10,.815,{x:oilHit.x+(1-settled)*basis*.17,y:oilHit.y-pan.w*(.31+.13*(1-settled))})}
 let poolOpacity=r(2.65,2.77)*(1-r(3.49,3.68));const pool={...pocket,rx:pan.w*.085,ry:pan.w*.029,o:poolOpacity};
 // Place the melting cube at the same pocket the spoon actually dips into.
 if(t>=2.35&&t<=2.81){const drop=r(2.49,2.68);butter.x=pocket.x;butter.y=pocket.y-(1-drop)*pan.w*.29}
 const enter=r(2.64,2.78),fill=r(2.78,2.86),lift=r(2.86,2.975),tilt=r(2.96,3.045),recover=r(3.35,3.48),leave=r(3.48,3.57);
 const load=fill*(1-r(3.02,3.34));spoon.o=enter*(1-leave);spoon.w=pan.w*.35;spoon.h=spoon.w*aspects.spoon;spoon.a=8*(1-lift)-16*tilt+18*recover;
 // Scoop with the bowl immersed, then lift with the bowl level; pivot about the pouring lip.
 const lip0=point(spoon,.16,.86),bowl=point(spoon,.22,.73),sx=pocket.x+lip0.x-bowl.x,sy=pocket.y+lip0.y-bowl.y;
 const lip={x:mix(sx,hit.x,lift)+(1-enter)*pan.w*.09+leave*pan.w*.06,y:mix(sy,hit.y-pan.w*.12,lift)-(1-enter)*pan.w*.065-Math.sin(Math.PI*lift)*pan.w*.055-leave*pan.w*.08};pin(spoon,.16,.86,lip);
 const streams=[];function down(source,landing,opacity,kind){if(opacity>0&&source.y<landing.y)streams.push({kind,x:source.x,y:source.y,bottom:landing.y,o:opacity})}
 down(point(oil,.10,.815),oilHit,r(1.5,1.535)*(1-r(1.71,1.74)),'oil');down(lip,hit,r(2.98,3.035)*(1-r(3.29,3.35)),'butter');
 const cue=t>=1.47&&t<1.82?'先下中性油':t>=2.49&&t<2.72?'转小火，加黄油':t>=2.72&&t<2.86?'从锅边舀起黄油':t>=2.86&&t<2.98?'抬勺至肉面上方':t>=2.98&&t<3.36?'倾勺，淋在肉面上':'';
 return {sprites,pool,load,streams,cue};
}
if(typeof module!=='undefined')module.exports=kitchenFrame;
