// Pass 3 of the screen-space fluid: normals from the smoothed depth, then
// refraction and absorption by thickness over the scene snapshot.
#include "SSFRCommon.hlsl"
Texture2D<float> fluidDepth:register(t100);
Texture2D<float> fluidThickness:register(t101);
Texture2D<float4> sceneLighting:register(t104);
struct ScreenVarying{float4 position:SV_Position;};
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
float depthAt(int2 pixel,int2 last){return fluidDepth.Load(int3(clamp(pixel,int2(0,0),last),0));}
// Surface tangent along one screen axis, differencing toward the neighbor
// closer in depth so silhouettes keep the normal of the surface they belong to.
float3 tangent(float3 position,float2 uv,float depth,float2 step,float forward,float backward){
 bool useForward=!noFluid(forward)&&(noFluid(backward)||abs(forward-depth)<=abs(backward-depth));
 if(useForward)return worldAt(uv+step,forward)-position;
 if(!noFluid(backward))return position-worldAt(uv-step,backward);
 return worldAt(uv+step,depth)-position;
}
float4 main(ScreenVarying i):SV_Target{
 uint width,height;fluidDepth.GetDimensions(width,height);
 int2 pixel=int2(i.position.xy),last=int2(width-1,height-1);
 float depth=depthAt(pixel,last);
 clip(NO_FLUID*.5-depth);
 float2 dims=float2(width,height),uv=(pixel+.5)/dims;
 float thickness=max(fluidThickness.Load(int3(pixel,0)),0);

 float3 position=worldAt(uv,depth);
 float3 dx=tangent(position,uv,depth,float2(1/dims.x,0),depthAt(pixel+int2(1,0),last),depthAt(pixel-int2(1,0),last));
 float3 dy=tangent(position,uv,depth,float2(0,1/dims.y),depthAt(pixel+int2(0,1),last),depthAt(pixel-int2(0,1),last));
 float3 eye=eyePosition(),toward=normalize(eye-position);
 float3 n=cross(dx,dy);
 n=length(n)>1e-12?normalize(n):toward;
 if(dot(n,toward)<0)n=-n;
 float4 projected=project(position);
 if(sceneInFront(uv,projected))clip(-1);

 float3 right=normalize(camera[4].xyz),up=normalize(camera[5].xyz);
 float3 tint=float3(.78,.51,.055);
 // Beer-Lambert absorption that reaches the body tint at 12 mm of liquid.
 float3 absorb=-log(max(tint,.02))/.012;
 float3 transmit=exp(-thickness*absorb);
 float2 refractionUV=uv+float2(dot(n,right),-dot(n,up))*.035*saturate(thickness/.008);
 float3 background=hdrBilinear(refractionUV);
 float3 ambient=(hdrAt(uv+float2(.04,0))+hdrAt(uv-float2(.04,0))+hdrAt(uv+float2(0,.04))+hdrAt(uv-float2(0,.04)))*.25;
 // Broad scene probes avoid treating dark clothing immediately behind the jet as the entire environment.
 float3 broad=(hdrAt(float2(.15,.15))+hdrAt(float2(.85,.15))+hdrAt(float2(.2,.55))+hdrAt(float2(.8,.55)))*.25;
 ambient=ambient*.6+broad*.4;
 float luminance=dot(ambient,float3(.2126,.7152,.0722));
 float illumination=saturate(luminance/(luminance+.18));
 float fresnel=.0204+.9796*pow(1-saturate(dot(n,toward)),5);
 float3 reflected=reflect(-toward,n);
 float2 reflectionUV=saturate(uv+float2(dot(reflected,right),-dot(reflected,up))*.19);
 float3 reflection=hdrBilinear(reflectionUV)*.6+hdrBilinear(reflectionUV+float2(.004,0))*.2+hdrBilinear(reflectionUV-float2(.004,0))*.2;
 // Approximate a compact key-light lobe; its intensity follows the captured scene, not a constant glow.
 float3 light=normalize(float3(-.4,.8,.2)),halfway=normalize(light+toward);
 float spec=pow(saturate(dot(n,halfway)),96),shoulder=pow(saturate(dot(n,halfway)),24);
 float3 glint=float3(1,.97,.84)*(spec*4.3+shoulder*.30)*illumination;
 float3 body=tint*(1-transmit)*(.55+.45*illumination);
 float3 color=background*transmit+body+reflection*fresnel*1.6+glint;
 // Coverage follows thickness, so silhouettes and thin spray fade instead of cutting.
 float coverage=1-exp(-thickness/.0025);
 float alpha=lerp(.30,.97,coverage);
 return float4(color,alpha);
}
#endif
