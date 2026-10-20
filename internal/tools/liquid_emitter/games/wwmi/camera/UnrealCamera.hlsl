// Builds the camera of Camera.hlsl from Unreal's view constants at b13, which
// hold the translated world-to-clip matrix from row IniParams[254].x and the
// pre-view translation, the negated eye position, in row IniParams[254].y.
// Unreal's world is in centimeters with Z up; a point (x, y, z) of it is
// (x, z, -y) / 100 in the runtime's world.
#include "Matrix.hlsl"
Texture1D<float4> IniParams:register(t120);
cbuffer Source:register(b13){float4 source[4096];}
RWStructuredBuffer<float4> camera:register(u0);

void store(uint row,float4x4 m){for(uint i=0;i<4;i++)camera[row+i]=m[i];}

[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID){
 uint first=(uint)IniParams[254].x;
 float3 translation=source[(uint)IniParams[254].y].xyz;
 // The clip coefficients of each runtime axis, relative to the eye.
 float4 axis[3]={source[first]*100,source[first+2]*100,-source[first+1]*100};
 float4 origin=source[first+3];
 // Clip w is the depth along the view direction in centimeters. Scaling the
 // whole of clip space leaves every projected point in place, and in meters
 // the projection holds the plain depth that the runtime compares with.
 float3 depth=float3(axis[0].w,axis[1].w,axis[2].w);
 float scale=length(depth);
 if(!(scale>1e-6))return;
 [unroll]for(uint k=0;k<3;k++)axis[k]/=scale;
 origin/=scale;

 // Split the view rotation off: the view axes are where clip x, y and w grow.
 float3 forward=depth/scale;
 float3 clipX=float3(axis[0].x,axis[1].x,axis[2].x);
 float3 clipY=float3(axis[0].y,axis[1].y,axis[2].y);
 float3 right=normalize(clipX-dot(clipX,forward)*forward);
 float3 up=normalize(clipY-dot(clipY,forward)*forward-dot(clipY,right)*right);
 float3 eye=float3(-translation.x,-translation.z,translation.y)*.01;
 float4x4 inverseView=float4x4(float4(right,0),float4(up,0),float4(forward,0),float4(eye,1));
 float4x4 view=float4x4(
  float4(right.x,up.x,forward.x,0),
  float4(right.y,up.y,forward.y,0),
  float4(right.z,up.z,forward.z,0),
  float4(-dot(eye,right),-dot(eye,up),-dot(eye,forward),1));
 float3 basis[3]={right,up,forward};
 float4x4 projection;
 [unroll]for(uint i=0;i<3;i++)projection[i]=basis[i].x*axis[0]+basis[i].y*axis[1]+basis[i].z*axis[2];
 projection[3]=origin;
 float4x4 vp=float4x4(axis[0],axis[1],axis[2],origin-eye.x*axis[0]-eye.y*axis[1]-eye.z*axis[2]);
 store(0,view);store(4,inverseView);store(8,projection);store(12,inverse(projection));store(16,vp);
 store(20,float4x4(.01,0,0,0,0,0,-.01,0,0,.01,0,0,0,0,0,1));
}
