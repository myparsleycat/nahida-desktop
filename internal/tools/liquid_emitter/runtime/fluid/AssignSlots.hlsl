// Match this frame's characters, as ResolveSurface reported them, to the
// slots that tracked them in earlier frames, so each keeps its own stream.
#define SLOT_STATE_WRITER
#include "ParticleConfig.hlsl"
Texture1D<float4> IniParams:register(t120);
#include "Slots.hlsl"
StructuredBuffer<float4> candidates:register(t0);
StructuredBuffer<float4> profiles:register(t1);
// FluidSolver's frame state, four rows per slot.
RWStructuredBuffer<float4> frameState:register(u1);
// How far a slot follows its character onto a different emitter id, past the
// motion predicted from the last two frames.
static const float FOLLOW_RADIUS=.5;
// Longer than any slot particle lives: stream .48 s, adhesion 1.6 s, puddle 2.4 s.
static const float SLOT_TIMEOUT=3.0;
static const uint NONE=0xffffffffu;
float3 candidateRoot(uint c){return candidates[1+c*CANDIDATE_ROWS+5].xyz;}
float candidateId(uint c){return candidates[1+c*CANDIDATE_ROWS+4].y;}
float3 expectedRoot(uint s){
 float3 root=slotRoot(s);
 float4 previous=slots[s*SLOT_ROWS+SLOT_PREVIOUS_ROOT];
 float3 motion=previous.w>.5?root-previous.xyz:0;
 return root+motion*min(1,FOLLOW_RADIUS/max(length(motion),1e-6));
}
[numthreads(1,1,1)]
void main(uint3 tid:SV_DispatchThreadID){
 float now=IniParams[239].y+IniParams[239].x;
 uint count=min((uint)candidates[0].x,CANDIDATE_CAPACITY);
 uint match[SLOT_COUNT];
 bool taken[CANDIDATE_CAPACITY];
 [unroll]for(uint c=0;c<CANDIDATE_CAPACITY;c++)taken[c]=false;
 [unroll]for(uint s=0;s<SLOT_COUNT;s++)match[s]=NONE;

 // Closest pairs first. The first phase keeps a slot on its own emitter id;
 // the second lets it follow its character to another one, e.g. an outfit
 // toggle that swaps the hooked mesh.
 [loop]for(uint phase=0;phase<2;phase++){
  [loop]for(uint attempt=0;attempt<SLOT_COUNT;attempt++){
   float best=1e30;uint bestSlot=NONE,bestCandidate=NONE;
   [loop]for(uint slot=0;slot<SLOT_COUNT;slot++){
    if(!slotLive(slot)||match[slot]!=NONE)continue;
    float3 expected=expectedRoot(slot);
    float id=slotAnchor(slot,4).y;
    [loop]for(uint candidate=0;candidate<count;candidate++){
     if(taken[candidate]||(phase==0&&candidateId(candidate)!=id))continue;
     float3 offset=candidateRoot(candidate)-expected;
     float distance2=dot(offset,offset);
     if(phase==1&&distance2>FOLLOW_RADIUS*FOLLOW_RADIUS)continue;
     if(distance2<best){best=distance2;bestSlot=slot;bestCandidate=candidate;}
    }
   }
   if(bestSlot==NONE)break;
   match[bestSlot]=bestCandidate;taken[bestCandidate]=true;
  }
 }

 // Characters no slot followed take the empty slots; the rest wait for one.
 [loop]for(uint fresh=0;fresh<count;fresh++){
  if(taken[fresh])continue;
  [loop]for(uint empty=0;empty<SLOT_COUNT;empty++){
   if(slotLive(empty)||match[empty]!=NONE)continue;
   match[empty]=fresh;taken[fresh]=true;break;
  }
 }

 [loop]for(uint target=0;target<SLOT_COUNT;target++){
  uint base=target*SLOT_ROWS;
  if(match[target]!=NONE){
   uint source=1+match[target]*CANDIDATE_ROWS;
   bool assigned=slotLive(target);
   slots[base+SLOT_PREVIOUS_ROOT]=assigned?float4(slotRoot(target),1):0;
   [unroll]for(uint k=0;k<CANDIDATE_ROWS;k++)slots[base+k]=candidates[source+k];
   slots[base+SLOT_ROOT]=float4(candidates[source+5].xyz,1);
   slots[base+SLOT_TRACK]=float4(now,0,0,0);
   // A profile reload changes the look of a character already in its slot.
   uint profileCount,stride;profiles.GetDimensions(profileCount,stride);
   uint row=(uint)candidates[source+4].y*PROFILE_ROWS;
   bool known=row+PROFILE_ROWS<=profileCount;
   slots[base+SLOT_LOOK]=known?profiles[row+3]:0;
   slots[base+SLOT_TINT]=known?profiles[row+4]:0;
   // A new character must not inherit the motion or nozzle of the last one.
   if(!assigned){frameState[target*4]=0;frameState[target*4+2]=0;}
  }else{
   float4 anchor=slots[base+4];
   slots[base+4]=float4(anchor.xyz,0);
   // Freeing last keeps the slot from being reused in the frame its
   // particles are dropped.
   if(slotLive(target)&&now-slots[base+SLOT_TRACK].x>SLOT_TIMEOUT){
    slots[base+SLOT_ROOT]=0;slots[base+SLOT_PREVIOUS_ROOT]=0;
   }
  }
 }
}
