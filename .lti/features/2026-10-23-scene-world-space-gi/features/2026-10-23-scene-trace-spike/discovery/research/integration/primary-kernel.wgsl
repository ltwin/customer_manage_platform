// Three.js r186 - Node System

// directives
enable subgroups;

// system
var<private> instanceIndex : u32;

// locals


// structs

struct Ray {
	origin : vec3f,
	direction : vec3f,
	maxDist : f32
};

struct IntersectionResult {
	indices : vec4u,
	normal : vec3f,
	didHit : bool,
	barycoord : vec3f,
	objectIndex : u32,
	side : f32,
	dist : f32
};

struct BVHBoundingBox {
	min : array<f32, 3>,
	max : array<f32, 3>
};

struct BVHNode {
	bounds : BVHBoundingBox,
	rightChildOrTriangleOffset : u32,
	splitAxisOrTriangleCount : u32
};

struct TransformStruct {
	matrixWorld : mat4x4f,
	inverseMatrixWorld : mat4x4f,
	visible : u32,
	_alignment0 : u32,
	_alignment1 : u32,
	_alignment2 : u32
};

struct bvh_GeometryStruct {
	position : vec4f,
	normal : vec4f
};


// uniforms

struct bvh_transformsStruct {
	value : array< TransformStruct >
};
@binding( 1 ) @group( 0 )
var<storage, read> bvh_transforms : bvh_transformsStruct;

struct bvh_nodesStruct {
	value : array< BVHNode >
};
@binding( 2 ) @group( 0 )
var<storage, read> bvh_nodes : bvh_nodesStruct;

struct bvh_indexStruct {
	value : array< u32 >
};
@binding( 3 ) @group( 0 )
var<storage, read> bvh_index : bvh_indexStruct;

struct bvh_attributesStruct {
	value : array< bvh_GeometryStruct >
};
@binding( 4 ) @group( 0 )
var<storage, read> bvh_attributes : bvh_attributesStruct;

struct NodeBuffer_10665Struct {
	value : array< vec4<f32> >
};
@binding( 5 ) @group( 0 )
var<storage, read_write> NodeBuffer_10665 : NodeBuffer_10665Struct;

struct NodeBuffer_10666Struct {
	value : array< u32 >
};
@binding( 6 ) @group( 0 )
var<storage, read_write> NodeBuffer_10666 : NodeBuffer_10666Struct;

struct objectStruct {
	nodeUniform0 : vec3<f32>,
	nodeUniform1 : mat4x4<f32>,
	nodeUniform2 : mat4x4<f32>,
	nodeUniform9 : u32
};
@binding( 0 ) @group( 0 )
var<uniform> object : objectStruct;

// vars
var<private> nodeVar0 : vec4<f32>;
var<private> bvh_rayScalar_dat3v : f32;
var<private> primary : IntersectionResult;

// codes
fn bvh_RaycastFirstHit( shape: Ray, result: ptr<function, IntersectionResult> ) -> bool {

			

			var didHit = false;

			var isTLAS = true;
			var pointer: i32 = 0;
			var stack: array<u32, 60u>;
			stack[ 0 ] = 0u;

			var blasDidHit: bool = false;
			var objectIndex: u32 = 0;
			var localShape: Ray = shape;

						var tlasReset: i32 = 0;

			loop {

												if ( ! isTLAS && tlasReset == pointer ) {

					if ( blasDidHit ) {

						blasDidHit = false;
						didHit = true;
						transformResult( result, objectIndex );

					}

					resetRayScalar( objectIndex );

					objectIndex = 0;
					isTLAS = true;
					localShape = shape;

				}

								if ( pointer < 0 || pointer >= i32( 60u ) ) {

					break;

				}

				let nodeIndex = stack[ pointer ];
				let node = bvh_nodes.value[ nodeIndex ];
				pointer = pointer - 1;

								if ( rayIntersectsBounds( localShape, node.bounds, result ) == 0u ) {

					continue;

				}

				let infoX = node.splitAxisOrTriangleCount;
				let infoY = node.rightChildOrTriangleOffset;
				let isLeaf = ( infoX & 0xffff0000u ) != 0u;

				if ( isLeaf ) {

					if ( isTLAS ) {

																								objectIndex = infoX & 0x00ffffffu;

						let transform = bvh_transforms.value[ objectIndex ];
						if ( transform.visible != 0u ) {

							tlasReset = pointer;
							isTLAS = false;
							blasDidHit = false;

														localShape = shape;
							transformRay( &localShape, objectIndex );

							pointer = pointer + 1;
							stack[ pointer ] = infoY;

						}

					} else {

						let count = infoX & 0x0000ffffu;
						let offset = infoY;
						blasDidHit = intersectRange( localShape, offset, count, result ) || blasDidHit;

					}

				} else {

					let leftIndex = nodeIndex + 1u;
					let splitAxis = infoX & 0x0000ffffu;
					let rightIndex = nodeIndex + infoY;

					var c1 = rightIndex;
					var c2 = leftIndex;
					
			let leftToRight = getBoundsOrder( localShape, splitAxis, node );
			c1 = select( rightIndex, leftIndex, leftToRight );
			c2 = select( leftIndex, rightIndex, leftToRight );
		

					pointer = pointer + 1;
					stack[ pointer ] = c2;

					pointer = pointer + 1;
					stack[ pointer ] = c1;

				}

			}

			return didHit;

		}
fn transformResult( hit: ptr<function, IntersectionResult>, objectIndex: u32 )  {

				let toLocal = bvh_transforms.value[ objectIndex ].inverseMatrixWorld;
				hit.normal = normalize( ( transpose( toLocal ) * vec4f( hit.normal, 0.0 ) ).xyz );
				hit.objectIndex = objectIndex;

			}
fn resetRayScalar( objectIndex: u32 )  {

				bvh_rayScalar_dat3v = 1.0;

			}
fn rayIntersectsBounds( ray: Ray, bounds: BVHBoundingBox, result: ptr<function, IntersectionResult> ) -> u32 {

				let boundsMin = vec3( bounds.min[0], bounds.min[1], bounds.min[2] );
				let boundsMax = vec3( bounds.max[0], bounds.max[1], bounds.max[2] );

				let invDir = 1.0 / ray.direction;
				let tMinPlane = ( boundsMin - ray.origin ) * invDir;
				let tMaxPlane = ( boundsMax - ray.origin ) * invDir;

				let tMinHit = vec3f(
					min( tMinPlane.x, tMaxPlane.x ),
					min( tMinPlane.y, tMaxPlane.y ),
					min( tMinPlane.z, tMaxPlane.z )
				);

				let tMaxHit = vec3f(
					max( tMinPlane.x, tMaxPlane.x ),
					max( tMinPlane.y, tMaxPlane.y ),
					max( tMinPlane.z, tMaxPlane.z )
				);

				let t0 = max( max( tMinHit.x, tMinHit.y ), tMinHit.z );
				let t1 = min( min( tMaxHit.x, tMaxHit.y ), tMaxHit.z );

				let dist = max( t0, 0.0 );
				if ( t1 < dist ) {

					return 0u;

				} else if ( ray.maxDist > 0.0 && dist * bvh_rayScalar_dat3v >= ray.maxDist ) {

					return 0u;

				} else if ( result.didHit && dist * bvh_rayScalar_dat3v >= result.dist ) {

					return 0u;

				} else {

					return 1u;

				}

			}
fn transformRay( ray: ptr<function, Ray>, objectIndex: u32 )  {

				let toLocal = bvh_transforms.value[ objectIndex ].inverseMatrixWorld;
				ray.origin = ( toLocal * vec4f( ray.origin, 1.0 ) ).xyz;
				ray.direction = ( toLocal * vec4f( ray.direction, 0.0 ) ).xyz;

				let len = length( ray.direction );
				ray.direction /= len;
				bvh_rayScalar_dat3v = 1.0 / len;

			}
fn intersectRange( ray: Ray, offset: u32, count: u32, result: ptr<function, IntersectionResult> ) -> bool {

				var didHit = false;
				for ( var ti = offset; ti < offset + count; ti = ti + 1u ) {

					let i0 = bvh_index.value[ ti * 3u ];
					let i1 = bvh_index.value[ ti * 3u + 1u ];
					let i2 = bvh_index.value[ ti * 3u + 2u ];

					let a = bvh_attributes.value[ i0 ].position.xyz;
					let b = bvh_attributes.value[ i1 ].position.xyz;
					let c = bvh_attributes.value[ i2 ].position.xyz;

					var triResult = intersectRayTriangle( ray, a, b, c, 0.0 );
					triResult.dist *= bvh_rayScalar_dat3v;
					if ( triResult.didHit && ( ray.maxDist <= 0.0 || triResult.dist < ray.maxDist ) && ( ! result.didHit || triResult.dist < result.dist ) ) {

						result.didHit = true;
						result.dist = triResult.dist;
						result.normal = triResult.normal;
						result.side = triResult.side;
						result.barycoord = triResult.barycoord;
						result.indices = vec4u( i0, i1, i2, ti );

						didHit = true;

					}

				}

				return didHit;

			}
fn intersectRayTriangle( ray: Ray, a: vec3f, b: vec3f, c: vec3f, threshold: f32 ) -> IntersectionResult {

		const DET_EPSILON = 1e-15;

		var result: IntersectionResult;
		result.didHit = false;

		let edge1 = b - a;
		let edge2 = c - a;
		let n = cross( edge1, edge2 );

		let det = - dot( ray.direction, n );
		if ( abs( det ) < DET_EPSILON ) {

			return result;

		}

		let invdet = 1.0 / det;

		let AO = ray.origin - a;
		let DAO = cross( AO, ray.direction );

		let u = dot( edge2, DAO ) * invdet;
		if ( u < 0.0 || u > 1.0 ) {

			return result;

		}

		let v = - dot( edge1, DAO ) * invdet;
		if ( v < 0.0 || u + v > 1.0 ) {

			return result;

		}

		let t = dot( AO, n ) * invdet;
		let w = 1.0 - u - v;
		if ( t < threshold ) {

			return result;

		}

		result.didHit = true;
		result.barycoord = vec3f( w, u, v );
		result.dist = t;
		result.side = sign( det );
		result.normal = result.side * normalize( n );

		return result;

	}
fn getBoundsOrder( ray: Ray, splitAxis: u32, node: BVHNode ) -> bool {

				return ray.direction[ splitAxis ] >= 0.0;

			}
fn spike_traceFirst ( origin: vec3f, direction: vec3f ) -> IntersectionResult {
      var ray: Ray;
      ray.origin = origin;
      ray.direction = direction;
      ray.maxDist = 0.0;
      var hit: IntersectionResult;
      bvh_RaycastFirstHit( ray, &hit );
      return hit;
    }



@compute @workgroup_size( 64, 1, 1 )
fn main( @builtin( global_invocation_id ) globalId : vec3<u32>,
	@builtin( workgroup_id ) workgroupId : vec3<u32>,
	@builtin( local_invocation_id ) localId : vec3<u32>,
	@builtin( num_workgroups ) numWorkgroups : vec3<u32>,
	@builtin( subgroup_size ) subgroupSize : u32 ) {

	// local vars
	

	// system
	instanceIndex = globalId.x
		+ globalId.y * ( 64 * numWorkgroups.x )
		+ globalId.z * ( 64 * numWorkgroups.x ) * ( 1 * numWorkgroups.y );

	// flow
	// code

	if ( instanceIndex >= object.nodeUniform9 ) { return; }

	nodeVar0 = ( object.nodeUniform2 * vec4<f32>( vec2<f32>( ( ( ( ( f32( ( instanceIndex % 1536u ) ) + 0.5 ) / 1536.0 ) * 2.0 ) - 1.0 ), ( 1.0 - ( ( ( f32( ( instanceIndex / 1536u ) ) + 0.5 ) / 1024.0 ) * 2.0 ) ) ), 0.5, 1.0 ) );
	bvh_rayScalar_dat3v = 1.0;
	primary = spike_traceFirst( object.nodeUniform0, normalize( ( ( object.nodeUniform1 * vec4<f32>( ( nodeVar0.xyz / vec3<f32>( nodeVar0.w ) ), 1.0 ) ).xyz - object.nodeUniform0 ) ) );

	if ( primary.didHit ) {

		NodeBuffer_10665.value[ instanceIndex ] = vec4<f32>( primary.dist, primary.normal );
		NodeBuffer_10666.value[ instanceIndex ] = bvh_transforms.value[ primary.objectIndex ]._alignment0;
		

	} else {

		NodeBuffer_10665.value[ instanceIndex ] = vec4<f32>( -1.0, 0.0, 0.0, 0.0 );
		NodeBuffer_10666.value[ instanceIndex ] = 4294967295u;
		

	}


	

}
