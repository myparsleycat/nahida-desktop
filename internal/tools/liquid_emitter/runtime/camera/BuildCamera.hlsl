// Builds the camera of Camera.hlsl from a copy of the game's camera constants
// at b13. IniParams[254] names the first row of the view matrix, its inverse,
// the projection and its inverse in those constants, and IniParams[255].x the
// first row of the view-projection, each stored as its four columns. -1 marks
// a matrix the game does not provide, which is derived from the others; the
// projection or the view-projection is required. Without the view and its
// inverse, the view-projection is split into view and projection. The game's
// world is the runtime's, so the game-to-world rows hold the identity.
#include "Matrix.hlsl"
#include "SplitViewProjection.hlsl"
Texture1D<float4> IniParams:register(t120);
cbuffer Source:register(b13){float4 source[4096];}
RWStructuredBuffer<float4> camera:register(u0);

// Matrices are handled transposed: row i of a float4x4 here is column i of
// the game's matrix, so inverse() and mul() work on transposes throughout.
float4x4 load(int row){return float4x4(source[row],source[row+1],source[row+2],source[row+3]);}
void store(uint row,float4x4 m){for(uint i=0;i<4;i++)camera[row+i]=m[i];}

[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID){
 int4 rows=(int4)IniParams[254];
 int viewProjection=(int)IniParams[255].x;
 float4x4 view,inverseView,projection;
 if(rows.x<0&&rows.y<0){
  if(!splitViewProjection(load(viewProjection),view,inverseView,projection))return;
 }else{
  view=rows.x>=0?load(rows.x):inverse(load(rows.y));
  inverseView=rows.y>=0?load(rows.y):inverse(view);
  // Transposed, P*V is V'*P', and P = (P*V)*inverse(V) is inverse(V)'*(P*V)'.
  projection=rows.z>=0?load(rows.z):mul(inverseView,load(viewProjection));
 }
 float4x4 inverseProjection=rows.w>=0?load(rows.w):inverse(projection);
 float4x4 vp=viewProjection>=0?load(viewProjection):mul(view,projection);
 store(0,view);store(4,inverseView);store(8,projection);store(12,inverseProjection);store(16,vp);
 store(20,float4x4(1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1));
}
