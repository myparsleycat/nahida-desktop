// Builds the camera of Camera.hlsl from a copy of the constants at b13 that a
// Zenless Zone Zero shader binds as its own globals. Their layout differs from
// shader to shader and does not always hold the view matrix, so the first
// IniParams[254].x rows are searched for the view-projection, which is split
// into view and projection. The game's world is the runtime's, so the
// game-to-world rows hold the identity.
#include "Matrix.hlsl"
#include "SplitViewProjection.hlsl"
Texture1D<float4> IniParams:register(t120);
cbuffer Source:register(b13){float4 source[4096];}
RWStructuredBuffer<float4> camera:register(u0);

void store(uint row,float4x4 m){for(uint i=0;i<4;i++)camera[row+i]=m[i];}

// Unity stores a matrix as its four columns, so row k of the matrix is
// component k of each. In a reversed-Z perspective view-projection the w row
// is the unit view direction, the z row nearly vanishes and x and y are almost
// perpendicular to w. The plain projection stored next to it has no
// translation, and the oblique one of a planar reflection a large z row.
bool isViewProjection(float4 c0,float4 c1,float4 c2,float4 c3){
 float3 x=float3(c0.x,c1.x,c2.x),y=float3(c0.y,c1.y,c2.y);
 float3 z=float3(c0.z,c1.z,c2.z),w=float3(c0.w,c1.w,c2.w);
 if(abs(length(w)-1)>1e-3||length(x)<.3||length(y)<.3||length(z)>.1)return false;
 if(abs(dot(normalize(x),w))>.02||abs(dot(normalize(y),w))>.02)return false;
 return abs(c3.x)+abs(c3.y)+abs(c3.w)>1e-4;
}

[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID){
 uint rows=min((uint)IniParams[254].x,4093),first=rows;
 [loop]for(uint r=0;r<rows;r++){
  if(isViewProjection(source[r],source[r+1],source[r+2],source[r+3])){first=r;break;}
 }
 if(first==rows)return;

 float4x4 view,inverseView,projection;
 if(!splitViewProjection(float4x4(source[first],source[first+1],source[first+2],source[first+3]),view,inverseView,projection))return;
 // The view-projection is stored rebuilt from its parts: fxc compiles a
 // store of vp itself to a loop that reads its first column four times.
 store(0,view);store(4,inverseView);store(8,projection);store(12,inverse(projection));
 store(16,mul(view,projection));
 store(20,float4x4(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1));
}
