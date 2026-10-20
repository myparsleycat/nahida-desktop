// The camera every runtime pass reads, which camera/BuildCamera.hlsl builds
// from the game's own constants. Rows 0-3 hold the view matrix, 4-7 its
// inverse, 8-11 the projection, 12-15 its inverse and 16-19 the
// view-projection, each matrix as its four columns: row 7 is the eye position
// and row 8.x the projection's horizontal scale. World space is in meters
// with Y up, which gravity and the root checks in ResolveSurface.hlsl assume.
#ifndef LIQUID_CAMERA
#define LIQUID_CAMERA
StructuredBuffer<float4> camera:register(t113);
#endif
