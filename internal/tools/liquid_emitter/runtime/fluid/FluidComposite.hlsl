// Pass 3 of the screen-space fluid: normals from the smoothed depth, then
// refraction and absorption by thickness over the scene snapshot.
#include "SSFRCommon.hlsl"
Texture2D<float> fluidDepth:register(t100);
Texture2D<float> fluidThickness:register(t101);
// rgb the thickness-weighted absorption of every liquid the pixel crosses,
// a their weight, and their absorption depth in mm under the same weight
// (FluidSurface.hlsl); unsmoothed.
Texture2D<float4> fluidColor:register(t102);
Texture2D<float> fluidAbsorptionDepth:register(t103);
Texture2D<float4> sceneLighting:register(t104);
// Row 0 the light from above, row 1 the broad surroundings (FluidAmbient.hlsl).
StructuredBuffer<float4> fluidAmbient:register(t105);
// Scattering and roughness under the weight in fluidColor's alpha
// (FluidSurface.hlsl); unsmoothed.
Texture2D<float2> fluidMaterial:register(t107);
struct ScreenVarying{float4 position:SV_Position;};
// Share of the offset lookup in the reflection a missed puddle ray keeps.
static const float REFLECTION_MISS_LOCAL=.3;
// Drops clinging to the body and their trails (TAG_FILM) are a film on it,
// facing whatever way it does: the thickness they saturate toward, how far
// their normals bend onto the body, how much of the key-light glint they
// lose, and their wet darkening as a share of a puddle's.
static const float FILM_LIMIT=.0018,FILM_NORMAL_PULL=.8,FILM_GLINT_CUT=.5,FILM_WET=.35;
// The film carries its liquid by color and a broad sheen along the body
// rather than by darkening: the sheen lobe is wide enough to catch the
// curve of a limb, and the drops on it scatter less so they do not stand
// out as bright beads against the trail.
static const float FILM_SHEEN=.3,FILM_SCATTER=.5;
// Film at which a trail fully shows: its coverage, sheen and darkening. A
// trail segment adds 1.6 mm on its center line, which smoothing spreads.
static const float FILM_SHOWN=.0008;
// Puddle thickness beyond about two overlapping lone discs, and the film a
// puddle reaches when liquid keeps gathering in it.
static const float PUDDLE_DEEP_START=.002,PUDDLE_DEEP_SCALE=.0015,PUDDLE_FILM_MAX=.0045;
// Scattering coefficient per meter at a scattering of 1, which goes with its
// square: at the translucent preset's .35 a 2 mm stream lets about 37
// percent through, at the cloudy .85 about 3 percent.
static const float SCATTERING_PER_METER=4000;
#ifdef VERTEX_SHADER
ScreenVarying main(uint id:SV_VertexID){ScreenVarying o;o.position=screenTriangle(id);return o;}
#endif
#ifdef PIXEL_SHADER
float3 hdrPixel(int2 pixel,int2 last){
 float3 color=sceneLighting.Load(int3(clamp(pixel,int2(0,0),last),0)).rgb;
 return all(isfinite(color))?clamp(color,0,64):0;
}
float3 hdrAt(float2 uv){
 uint w,h;sceneLighting.GetDimensions(w,h);if(w==0||h==0)return 0;
 return hdrPixel(int2(saturate(uv)*float2(w-1,h-1)),int2(w-1,h-1));
}
#ifdef LIQUID_SAMPLED
// The forked d3d11.dll binds a linear clamp sampler at s0 (fluid/Composite.ini).
SamplerState linearClamp:register(s0);
float3 hdrBilinear(float2 uv){
 float3 color=sceneLighting.SampleLevel(linearClamp,saturate(uv),0).rgb;
 return all(isfinite(color))?clamp(color,0,64):0;
}
#else
// A stock d3d11.dll gives custom shaders no sampler, so refraction interpolates by hand.
float3 hdrBilinear(float2 uv){
 uint w,h;sceneLighting.GetDimensions(w,h);if(w==0||h==0)return 0;
 float2 position=saturate(uv)*float2(w,h)-.5;
 int2 base=int2(floor(position)),last=int2(w-1,h-1);
 float2 fraction=position-base;
 float3 top=lerp(hdrPixel(base,last),hdrPixel(base+int2(1,0),last),fraction.x);
 float3 bottom=lerp(hdrPixel(base+int2(0,1),last),hdrPixel(base+int2(1,1),last),fraction.x);
 return lerp(top,bottom,fraction.y);
}
#endif
// Liquid of another character reads as empty, so its edge against this one
// is a silhouette rather than a slope.
float bodyDepthAt(int2 pixel,int2 last,uint tag){
 float raw=fluidDepth.Load(int3(clamp(pixel,int2(0,0),last),0));
 return noFluid(raw)||!sameFluidBody(tag,fluidDepthTag(raw))?NO_FLUID:fluidDepthValue(raw);
}
// Surface tangent along one screen axis, differencing toward the neighbor
// closer in depth so silhouettes keep the normal of the surface they belong to.
float3 tangent(float3 position,float2 uv,float depth,float2 step,float forward,float backward){
 bool useForward=!noFluid(forward)&&(noFluid(backward)||abs(forward-depth)<=abs(backward-depth));
 if(useForward)return worldAt(uv+step,forward)-position;
 if(!noFluid(backward))return position-worldAt(uv-step,backward);
 return worldAt(uv+step,depth)-position;
}
bool sceneTexelWorld(int2 texel,int2 last,out float3 world){
 world=0;
 texel=clamp(texel,int2(0,0),last);
 float d=sceneDepth.Load(int3(texel,0));
 if(!(isfinite(d)&&d>0&&d<1))return false;
 float2 uv=(float2(texel)+.5)/float2(last+1);
 float4 v=(uv.x*2-1)*camera[12]+(1-uv.y*2)*camera[13]+d*camera[14]+camera[15];
 if(abs(v.w)<=1e-8)return false;
 world=camera[7].xyz+(v.x*camera[4].xyz+v.y*camera[5].xyz+v.z*camera[6].xyz)/v.w;
 return all(isfinite(world));
}
// Plane of the scene geometry behind `uv`, the same reconstruction the
// solver uses to land puddles, so a puddle's gap to it is its film height.
bool sceneSurface(float2 uv,out float3 surfacePoint,out float3 normal){
 surfacePoint=0;normal=float3(0,1,0);
 uint w,h;sceneDepth.GetDimensions(w,h);if(w<3||h<3)return false;
 int2 last=int2(w-1,h-1),texel=clamp(int2(saturate(uv)*float2(w,h)),int2(0,0),last);
 float3 r,l,d,u;
 if(!sceneTexelWorld(texel,last,surfacePoint)||!sceneTexelWorld(texel+int2(1,0),last,r)||!sceneTexelWorld(texel-int2(1,0),last,l)
    ||!sceneTexelWorld(texel+int2(0,1),last,d)||!sceneTexelWorld(texel-int2(0,1),last,u))return false;
 float3 dx1=r-surfacePoint,dx2=surfacePoint-l,dy1=d-surfacePoint,dy2=surfacePoint-u;
 float3 dx=dot(dx1,dx1)<dot(dx2,dx2)?dx1:dx2,dy=dot(dy1,dy1)<dot(dy2,dy2)?dy1:dy2;
 float3 n=cross(dx,dy);float len=length(n);
 if(len<1e-10||length(dx)>.15||length(dy)>.15)return false;
 normal=n/len;if(dot(normal,camera[7].xyz-surfacePoint)<0)normal=-normal;
 return true;
}
// Screen-space reflection: march the ray through the scene depth with
// growing steps, then bisect the crossing. Misses keep `fallback`.
float3 traceReflection(float3 origin,float3 direction,float3 fallback){
 uint w,h;sceneDepth.GetDimensions(w,h);if(w==0||h==0)return fallback;
 float previous=0,t=.01;
 [loop]for(int k=0;k<30;k++){
  float4 c=project(origin+direction*t);
  if(c.w<=.05)break;
  float2 uv=screenUV(c);
  if(any(uv<0)||any(uv>1))break;
  float gap=behindScene(uv,c);
  // Skip the start's own floor and anything far behind a thin occluder.
  if(gap>.003&&gap<max(.04,(t-previous)*2)){
   float low=previous,high=t;
   [loop]for(int b=0;b<5;b++){
    float middle=(low+high)*.5;
    float4 m=project(origin+direction*middle);
    if(m.w>.05&&behindScene(screenUV(m),m)>0)high=middle;else low=middle;
   }
   float4 hit=project(origin+direction*high);
   float2 hitUV=saturate(screenUV(hit));
   float edge=saturate(min(min(hitUV.x,1-hitUV.x),min(hitUV.y,1-hitUV.y))/.05);
   return lerp(fallback,hdrBilinear(hitUV),edge);
  }
  previous=t;t*=1.22;
 }
 return fallback;
}
float4 main(ScreenVarying i):SV_Target{
 uint width,height;fluidDepth.GetDimensions(width,height);
 int2 pixel=int2(i.position.xy),last=int2(width-1,height-1);
 float raw=fluidDepth.Load(int3(pixel,0));
 clip(NO_FLUID*.5-raw);
 float depth=fluidDepthValue(raw);
 uint tag=fluidDepthTag(raw);
 float2 dims=float2(width,height),uv=(pixel+.5)/dims;
 float thickness=max(fluidThickness.Load(int3(pixel,0)),0);

 float3 position=worldAt(uv,depth);
 float3 dx=tangent(position,uv,depth,float2(1/dims.x,0),bodyDepthAt(pixel+int2(1,0),last,tag),bodyDepthAt(pixel-int2(1,0),last,tag));
 float3 dy=tangent(position,uv,depth,float2(0,1/dims.y),bodyDepthAt(pixel+int2(0,1),last,tag),bodyDepthAt(pixel-int2(0,1),last,tag));
 float3 eye=eyePosition(),toward=normalize(eye-position);
 float3 n=cross(dx,dy);
 n=length(n)>1e-12?normalize(n):toward;
 if(dot(n,toward)<0)n=-n;
 float4 projected=project(position);
 if(sceneInFront(uv,projected))clip(-1);
 // Liquid lying within a few millimeters of upward-facing ground is a
 // puddle. Its depth normals still carry the bumps of the individual discs,
 // so they are pulled onto the ground plane to read as one flat film.
 // A clinging film is pulled less, so a rivulet keeps some relief while its
 // drops stop catching a highlight each, as domes of their own would.
 float puddle=0,surfaceFilm=0;
 float3 groundPoint,groundNormal;
 if(sceneSurface(uv,groundPoint,groundNormal)){
  float height=abs(dot(position-groundPoint,groundNormal));
  float contact=1-smoothstep(.006,.012,height);
  puddle=smoothstep(.6,.8,groundNormal.y)*contact;
  surfaceFilm=(tag&TAG_FILM)!=0?(1-puddle)*contact:0;
  n=normalize(lerp(n,groundNormal,puddle*.9+surfaceFilm*FILM_NORMAL_PULL));
 }

 float3 right=normalize(camera[4].xyz),up=normalize(camera[5].xyz);
 // The look settings Probe.ini passes: 251 = tint and the absorption depth
 // in meters; 252 = wet ground darkening, puddle reflectance floor, stream
 // reflection gain and scattering; 253.x = refraction.
 float4 look=IniParams[251],shade=IniParams[252];
 // Beer-Lambert absorption that reaches the body tint at the absorption
 // depth, 15 mm by default. A film that darkens the ground much more reads
 // as oil, not urine. The default tint passes red and green almost equally
 // and absorbs blue: a urine yellow rather than amber, which over gray
 // ground turns brown. Each character's liquid brings its own absorption
 // (Slots.hlsl slotLiquid); where several overlap they mix by thickness.
 float4 mixed=fluidColor.Load(int3(pixel,0));
 float mixedDepth=fluidAbsorptionDepth.Load(int3(pixel,0));
 float depthMeters=max(look.w,.001);
 float3 absorb=-log(max(saturate(look.rgb),.02))/depthMeters;
 float2 material=0;
 if(mixed.a>0&&all(isfinite(mixed))&&isfinite(mixedDepth)){
  absorb=max(mixed.rgb/mixed.a,0);
  depthMeters=max(mixedDepth/mixed.a/1000,.001);
  float2 weighted=fluidMaterial.Load(int3(pixel,0));
  if(all(isfinite(weighted)))material=saturate(weighted/mixed.a);
 }
 uint view=debugView();
 [branch]if(view>=DEBUG_OOZE&&view<=DEBUG_OPTICS){
  // The particle passes wrote their debug colors where absorption goes.
  float3 shown=view==DEBUG_OPTICS?float3(saturate(thickness/.01),material):(mixed.a>0?saturate(mixed.rgb/mixed.a):0);
  return float4(shown,smoothstep(0,.0004,thickness));
 }
 float scattering=SCATTERING_PER_METER*material.x*material.x,roughness=material.y;
 // The body color at the liquid's own absorption depth: each character's
 // tint, and a thickness-weighted blend where liquids overlap.
 float3 tint=exp(-absorb*depthMeters);
 // Thickness is the sum of every overlapping particle's chord. A stream
 // keeps most of its chord, while a puddle stacks the discs of every
 // particle lying there and would read as centimeters where the real film
 // is a millimeter or two. Saturate each toward its own limit so the puddle
 // stays a film the ground shows through and a lone drop stays pale. Drops
 // gathered on the body would otherwise add up into a dark stain.
 float filmLimit=lerp(lerp(.005,.0015,puddle),FILM_LIMIT,surfaceFilm);
 float film=filmLimit*(1-exp(-thickness/filmLimit));
 // A grown puddle's disc carries a deeper film (Puddle.hlsl) than lone discs
 // overlap into, so only it passes the shallow limit above: it deepens toward
 // PUDDLE_FILM_MAX and takes on more of its color. A drop lying in it stays
 // as it was.
 float pooled=(tag&TAG_PUDDLE)!=0?puddle:0;
 film+=pooled*(PUDDLE_FILM_MAX-.0015)*(1-exp(-max(thickness-PUDDLE_DEEP_START,0)/PUDDLE_DEEP_SCALE));
 // Scattering turns light aside rather than absorbing it: the scene behind
 // dims further, and the liquid gives back the light around it in the
 // share each color is scattered rather than absorbed. It is lit, never lit
 // up: in the dark it adds nothing.
 float3 absorbed=exp(-film*absorb),transmit=absorbed*exp(-film*scattering);
 float3 albedo=scattering/max(absorb+scattering,1e-6);
 float2 refractionUV=uv+float2(dot(n,right),-dot(n,up))*IniParams[253].x*saturate(film/.004);
 float3 background=hdrBilinear(refractionUV);
 float3 ambient=(hdrAt(uv+float2(.04,0))+hdrAt(uv-float2(.04,0))+hdrAt(uv+float2(0,.04))+hdrAt(uv-float2(0,.04)))*.25;
 // Broad scene probes avoid treating dark clothing immediately behind the jet as the entire environment.
 float3 upperBroad=fluidAmbient[0].rgb,broad=fluidAmbient[1].rgb;
 ambient=ambient*.6+broad*.4;
 float luminance=dot(ambient,float3(.2126,.7152,.0722));
 float illumination=saturate(luminance/(luminance+.18));
 float fresnel=.0204+.9796*pow(1-saturate(dot(n,toward)),5);
 float3 reflected=reflect(-toward,n);
 float2 reflectionUV=saturate(uv+float2(dot(reflected,right),-dot(reflected,up))*.19);
 // Roughness spreads the reflection and the highlights over wider, dimmer
 // lobes; it changes nothing else.
 float spread=lerp(.004,.03,roughness);
 float3 reflection=hdrBilinear(reflectionUV)*.6+hdrBilinear(reflectionUV+float2(spread,0))*.2+hdrBilinear(reflectionUV-float2(spread,0))*.2;
 // The offset lookup above suits the curved stream; a flat puddle mirrors
 // what stands on it, which needs a traced ray.
 // A traced ray that leaves the screen finds nothing to mirror. The upper
 // screen stands in for what lies above, and the offset lookup keeps some of
 // the local color, so the film does not turn flat gray toward the edges.
 [branch]if(puddle>.01){
  float3 missed=lerp(lerp(ambient,upperBroad,saturate(reflected.y)),reflection,REFLECTION_MISS_LOCAL);
  reflection=lerp(reflection,traceReflection(position,reflected,missed),puddle*(1-.6*roughness));
 }
 // Approximate a compact key-light lobe; its intensity follows the captured scene, not a constant glow.
 float3 light=normalize(float3(-.4,.8,.2)),halfway=normalize(light+toward);
 float spec=pow(saturate(dot(n,halfway)),lerp(96,12,roughness))*lerp(1,.15,roughness);
 float shoulder=pow(saturate(dot(n,halfway)),lerp(24,6,roughness))*lerp(1,.3,roughness);
 // A narrow bright peak on every bead turns the stream into a string of
 // pearls; a lower peak over a wider shoulder reads as a wet sheen.
 float3 glint=float3(1,.97,.84)*(spec*2.2+shoulder*.35)*illumination;
 // No normal of a vertical stream faces that key light's half vector, so
 // the stream stayed matte while every drop sparkled. A soft lobe around
 // the view direction gives the cylinder the lengthwise sheen of a wet
 // surface without depending on where the game's light is.
 float3 sheenHalfway=normalize(normalize(toward+float3(0,.5,0))+toward);
 glint+=float3(1,.97,.84)*pow(saturate(dot(n,sheenHalfway)),lerp(32,8,roughness))*.45*lerp(1,.35,roughness)*illumination;
 // Both lobes are stand-ins for a light the scene does not provide. On the
 // curved stream they add a stripe; on a flat puddle every pixel shares one
 // normal, so from the mirror angle of either lobe the whole film turns
 // white. A puddle shines by the mirror image it traces instead.
 glint*=1-puddle;
 glint*=1-surfaceFilm*FILM_GLINT_CUT;
 glint+=float3(1,.97,.84)*pow(saturate(dot(n,sheenHalfway)),lerp(8,3,roughness))*FILM_SHEEN*lerp(1,.5,roughness)*illumination*surfaceFilm;
 // Clear liquid mostly absorbs: the scene behind darkens and takes the tint.
 // A little in-scattering keeps the color readable over dark backgrounds
 // without the liquid glowing like paint.
 float3 scatter=tint*(1-absorbed)*shade.w*illumination*(1-surfaceFilm*FILM_SCATTER);
 scatter+=(1-transmit)*albedo*ambient;
 // A lone droplet spans a few pixels, so its depth normals cover a whole
 // hemisphere and always catch the glint. Highlights need the body of the
 // stream or a large drop behind them, or spray turns into glitter. A
 // puddle's thickness is the sum of discs a millimeter thick, so its mirror
 // image is gated where the film exists rather than where discs stack.
 // A trail is thinner still and shines wherever it lies.
 float highlight=lerp(lerp(smoothstep(.003,.008,thickness),smoothstep(.0003,.0012,thickness),puddle),smoothstep(.0001,FILM_SHOWN,thickness),surfaceFilm);
 // Water filling the ground's pores darkens it under a puddle. Wet skin or
 // cloth darkens less, or the trail reads as a stain.
 float wet=puddle*saturate(film/.002)+FILM_WET*surfaceFilm*saturate(film/FILM_SHOWN);
 background*=lerp(1,shade.x,wet);
 // From standing height Fresnel alone gives a puddle about 6 percent
 // reflectance, which disappears into the film; a puddle reads as liquid by
 // its mirror image, so it keeps a floor, 15 percent by default.
 float reflectance=lerp(fresnel,fresnel+shade.y*(1-fresnel),puddle)*highlight;
 float reflectionGain=lerp(shade.z,1,puddle);
 float3 color=(background*transmit+scatter)*(1-reflectance)+reflection*reflectance*reflectionGain+glint*highlight;
 // The color already carries the scene behind it, so the interior covers
 // fully; only silhouettes, where smoothing thins the liquid out, fade. A
 // puddle's thickness is the sum of soft discs, which a threshold turns
 // into one film with an irregular edge, and lone drops into small dots.
 float alpha=lerp(lerp(1-exp(-thickness/.0006),smoothstep(.00015,.0005,thickness),puddle),smoothstep(.00005,FILM_SHOWN,thickness),surfaceFilm);
 return float4(color,alpha);
}
#endif
