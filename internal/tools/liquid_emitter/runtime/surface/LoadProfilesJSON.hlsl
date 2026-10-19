// JSON byte buffers are loaded by EFMI. Parse once after each F10 reload.
// Accepted runtime records: [global_id, 12 profile floats, 3 local offset floats],
// optionally followed by the mod's look: strength offset, custom color flag,
// tint rgb and absorption depth in meters. Without it the look rows stay zero,
// which means the global settings.
#include "../fluid/ParticleConfig.hlsl"
static const uint RECORD_BASE=16,RECORD_LOOK=22;
Buffer<uint> jsonBytes:register(t0);
RWStructuredBuffer<float4> profiles:register(u0);
RWStructuredBuffer<float4> offsets:register(u1);

uint at(uint p,uint size){return p<size?jsonBytes[p]:0;}
bool white(uint c){return c==32||c==9||c==10||c==13;}
void spaces(inout uint p,uint size){[loop]while(p<size&&white(at(p,size)))p++;}
bool take(inout uint p,uint size,uint c){spaces(p,size);if(at(p,size)!=c)return false;p++;return true;}
bool digit(uint c){return c>=48&&c<=57;}
bool number(inout uint p,uint size,out float value){
 spaces(p,size);value=0;float sign=1;
 if(at(p,size)==45){sign=-1;p++;}
 if(!digit(at(p,size)))return false;
 uint digits=0;
 [loop]while(digit(at(p,size))&&digits<32){value=value*10+(at(p,size)-48);p++;digits++;}
 if(at(p,size)==46){p++;float scale=.1;if(!digit(at(p,size)))return false;
  [loop]while(digit(at(p,size))&&digits<48){value+=(at(p,size)-48)*scale;scale*=.1;p++;digits++;}}
 if(at(p,size)==101||at(p,size)==69){p++;float esign=1;
  if(at(p,size)==45){esign=-1;p++;}else if(at(p,size)==43)p++;
  if(!digit(at(p,size)))return false;float exponent=0;uint edigits=0;
  [loop]while(digit(at(p,size))&&edigits<3){exponent=exponent*10+(at(p,size)-48);p++;edigits++;}
  if(exponent>45)return false;value*=pow(10,esign*exponent);}
 value*=sign;return isfinite(value);
}
bool enabledKey(uint p,uint n){
 return at(p,n)==34&&at(p+1,n)==101&&at(p+2,n)==110&&at(p+3,n)==97&&at(p+4,n)==98&&at(p+5,n)==108&&at(p+6,n)==101&&at(p+7,n)==100&&at(p+8,n)==34;
}
bool dataKey(uint p,uint n){
 return at(p,n)==34&&at(p+1,n)==103&&at(p+2,n)==112&&at(p+3,n)==117&&at(p+4,n)==95&&at(p+5,n)==100&&at(p+6,n)==97&&at(p+7,n)==116&&at(p+8,n)==97&&at(p+9,n)==34;
}
bool records(uint p,uint size,bool writeRows){
 if(!take(p,size,91))return false;
 uint capacity,stride,offsetCapacity;profiles.GetDimensions(capacity,stride);offsets.GetDimensions(offsetCapacity,stride);
 [loop]for(uint record=0;record<4096;record++){
  spaces(p,size);if(at(p,size)==93)return true;
  if(!take(p,size,91))return false;
  float v[RECORD_LOOK];
  [unroll]for(uint d=RECORD_BASE;d<RECORD_LOOK;d++)v[d]=0;
  uint count=0;
  [loop]while(count<RECORD_LOOK){
   if(!number(p,size,v[count]))return false;
   count++;
   if(!take(p,size,44))break;
  }
  if((count!=RECORD_BASE&&count!=RECORD_LOOK)||!take(p,size,93))return false;
  if(v[0]<0||v[0]>=4096||v[0]!=floor(v[0]))return false;
  uint id=(uint)v[0],row=id*PROFILE_ROWS;if(row+PROFILE_ROWS>capacity||id>=offsetCapacity)return false;
  if(any(abs(float3(v[13],v[14],v[15]))>.25))return false;
  if(v[1]<=100&&(min(v[2],min(v[3],v[4]))<-.00001||abs(v[2]+v[3]+v[4]-1)>.001))return false;
  if(writeRows){
   profiles[row]=float4(v[1],v[2],v[3],v[4]);
   profiles[row+1]=float4(v[5],v[6],v[7],v[8]);
   profiles[row+2]=float4(v[9],v[10],v[11],v[12]);
   profiles[row+3]=float4(v[16],v[17],v[21],0);
   profiles[row+4]=float4(v[18],v[19],v[20],0);
   offsets[id]=float4(v[13],v[14],v[15],0);
  }
  spaces(p,size);if(at(p,size)==93)return true;if(!take(p,size,44))return false;
 }
 return false;
}
[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID){
 uint size;jsonBytes.GetDimensions(size);if(size<32||size>4194304)return;
 uint start=0;bool enabled=false;
 // Writer puts these two keys first; do not inspect user-facing metadata later in the file.
 [loop]for(uint p=0;p<min(size,2048u);p++){
  if(enabledKey(p,size)){uint c=p+9;if(!take(c,size,58))return;spaces(c,size);
   enabled=at(c,size)==116&&at(c+1,size)==114&&at(c+2,size)==117&&at(c+3,size)==101;}
  if(dataKey(p,size)){start=p+10;if(!take(start,size,58))return;break;}
 }
 if(!enabled||start==0)return;
 // Validate the whole runtime array before writing any live profile for this model.
 if(records(start,size,false))records(start,size,true);
}
