// Fork of three r186 (0.186.1) examples/jsm/tsl/display/GTAONode.js
// (patterns「场景着色写法」: compare with upstream on every three upgrade).
// Changed, each marked "fork:":
//  - the AO itself, between "fork: begin" and "fork: end": r186 moved the
//    node to a new horizon search (quadratic steps, each sample fading out
//    towards the radius), which leaves contacts and corners about half as
//    dark as the WebGL GTAOPass drew them; the block is GTAOShader's search
//    and integral, so the workbench keeps its look (R1: clay exports within
//    0.25% of pixels off by more than 8, against 3–8% with r186's);
//  - the view direction for orthographic cameras: three takes it as the way
//    back to the camera's origin, which only holds for a perspective camera;
//    an orthographic one looks along -Z everywhere.
// three's lines above the class are kept as they are, names the fork no
// longer uses included, so the comparison stays exact.
/* eslint-disable no-unused-vars */

import { DataTexture, RenderTarget, RepeatWrapping, Vector2, Vector3, TempNode, QuadMesh, NodeMaterial, RendererUtils, RedFormat } from 'three/webgpu';
import { reference, logarithmicDepthToViewZ, viewZToPerspectiveDepth, getNormalFromDepth, getViewPosition, getScreenPositionFromClip, nodeObject, Fn, float, NodeUpdateType, uv, uniform, Loop, vec2, vec3, vec4, int, dot, max, min, pow, abs, If, textureSize, sin, cos, PI, texture, passTexture, mat3, normalize, cross, mix, acos, clamp, interleavedGradientNoise, screenCoordinate, rand, context } from 'three/tsl';

const _quadMesh = /*@__PURE__*/ new QuadMesh();
const _size = /*@__PURE__*/ new Vector2();

// From Activision GTAO paper: https://www.activision.com/cdn/research/s2016_pbs_activision_occlusion.pptx
const _temporalRotations = [ 60, 300, 180, 240, 120, 0 ];
const _spatialOffsets = [ 0, 0.5, 0.25, 0.75 ];

let _rendererState;

/**
 * Post processing node for applying Ground Truth Ambient Occlusion (GTAO) to a scene.
 * ```js
 * const renderPipeline = new THREE.RenderPipeline( renderer );
 *
 * // pre-pass for normals and depth
 *
 * const prePass = pass( scene, camera );
 * prePass.setMRT( mrt( {
 * 	output: normalView
 * } ) );
 *
 * const prePassNormal = prePass.getTextureNode();
 * const prePassDepth = prePass.getTextureNode( 'depth' );
 *
 * // scene pass
 *
 * const scenePass = pass( scene, camera );
 *
 * // ao
 *
 * const aoPass = ao( prePassDepth, prePassNormal, camera );
 * const aoPassOutput = aoPass.getTextureNode();
 *
 * // apply the ambient occlusion to the scene
 *
 * scenePass.contextNode = builtinAOContext( aoPassOutput.sample( screenUV ).r );
 *
 * renderPipeline.outputNode = scenePass;
 * ```
 *
 * Reference: [Practical Real-Time Strategies for Accurate Indirect Occlusion](https://www.activision.com/cdn/research/Practical_Real_Time_Strategies_for_Accurate_Indirect_Occlusion_NEW%20VERSION_COLOR.pdf).
 *
 * @augments TempNode
 * @three_import import { ao } from 'three/addons/tsl/display/GTAONode.js';
 */
class GTAONode extends TempNode {

	static get type() {

		return 'GTAONode';

	}

	/**
	 * Constructs a new GTAO node.
	 *
	 * @param {Node<float>} depthNode - A node that represents the scene's depth.
	 * @param {?Node<vec3>} normalNode - A node that represents the scene's normals.
	 * @param {Camera} camera - The camera the scene is rendered with.
	 */
	constructor( depthNode, normalNode, camera ) {

		super( 'float' );

		/**
		 * A node that represents the scene's depth.
		 *
		 * @type {Node<float>}
		 */
		this.depthNode = depthNode;

		/**
		 * A node that represents the scene's normals. If no normals are passed to the
		 * constructor (because MRT is not available), normals can be automatically
		 * reconstructed from depth values in the shader.
		 *
		 * @type {?Node<vec3>}
		 */
		this.normalNode = normalNode;

		/**
		 * The resolution scale. By default the effect is rendered in full resolution
		 * for best quality but a value of `0.5` should be sufficient for most scenes.
		 *
		 * @type {number}
		 * @default 1
		 */
		this.resolutionScale = 1;

		/**
		 * The `updateBeforeType` is set to `NodeUpdateType.FRAME` since the node renders
		 * its effect once per frame in `updateBefore()`.
		 *
		 * @type {string}
		 * @default 'frame'
		 */
		this.updateBeforeType = NodeUpdateType.FRAME;

		/**
		 * The render target the ambient occlusion is rendered into.
		 *
		 * @private
		 * @type {RenderTarget}
		 */
		this._aoRenderTarget = new RenderTarget( 1, 1, { depthBuffer: false, format: RedFormat } );
		this._aoRenderTarget.texture.name = 'GTAONode.AO';

		// uniforms

		/**
		 * The radius of the ambient occlusion.
		 *
		 * @type {UniformNode<float>}
		 */
		this.radius = uniform( 0.25 );

		/**
		 * The thickness of the ambient occlusion.
		 *
		 * @type {UniformNode<float>}
		 */
		this.thickness = uniform( 1 );

		/**
		 * @deprecated since r186. The new distance model "Quadratic Ray Stepping"
		 * does not need it anymore.
		 *
		 * @type {UniformNode<float>}
		 */
		this.distanceExponent = uniform( 1 );

		/**
		 * @deprecated since r186. The new distance model "Quadratic Ray Stepping"
		 * does not need it anymore.
		 *
		 * @type {UniformNode<float>}
		 */
		this.distanceFallOff = uniform( 1 );

		/**
		 * The scale of the ambient occlusion.
		 *
		 * @type {UniformNode<float>}
		 */
		this.scale = uniform( 1 );

		/**
		 * How many samples are used to compute the AO.
		 * A higher value results in better quality but also
		 * in a more expensive runtime behavior.
		 *
		 * Note: Changing this member triggers a shader recompilation.
		 *
		 * @type {UniformNode<float>}
		 */
		this.samples = uniform( 16 );

		/**
		 * Whether to use temporal filtering or not. Setting this property to
		 * `true` requires the usage of `TRAANode`. This will help to reduce noise
		 * although it introduces typical TAA artifacts like ghosting and temporal
		 * instabilities.
		 *
		 * If setting this property to `false`, a manual denoise via `DenoiseNode`
		 * might be required.
		 *
		 * @type {boolean}
		 * @default false
		 */
		this.useTemporalFiltering = false;

		/**
		 * The resolution of the effect. Can be scaled via `resolutionScale`.
		 *
		 * @private
		 * @type {UniformNode<vec2>}
		 */
		this._resolution = uniform( new Vector2() );

		/**
		 * The internal noise texture used by the AO.
		 *
		 * @private
		 * @type {DataTexture}
		 */
		this._noiseTexture = generateMagicSquareNoise();

		/**
		 * The node represents the internal noise texture used by the AO.
		 *
		 * @private
		 * @type {TextureNode}
		 */
		this._noiseNode = texture( this._noiseTexture );

		/**
		 * Represents the projection matrix of the scene's camera.
		 *
		 * @private
		 * @type {UniformNode<mat4>}
		 */
		this._cameraProjectionMatrix = uniform( camera.projectionMatrix );

		/**
		 * Represents the inverse projection matrix of the scene's camera.
		 *
		 * @private
		 * @type {UniformNode<mat4>}
		 */
		this._cameraProjectionMatrixInverse = uniform( camera.projectionMatrixInverse );

		// fork: whether the camera is orthographic; a pipeline is built per camera.
		this._orthographic = uniform( camera.isOrthographicCamera === true );

		/**
		 * Represents the near value of the scene's camera.
		 *
		 * @private
		 * @type {ReferenceNode<float>}
		 */
		this._cameraNear = reference( 'near', 'float', camera );

		/**
		 * Represents the far value of the scene's camera.
		 *
		 * @private
		 * @type {ReferenceNode<float>}
		 */
		this._cameraFar = reference( 'far', 'float', camera );

		/**
		 * Temporal direction that influences the rotation angle for each slice.
		 *
		 * @private
		 * @type {UniformNode<float>}
		 */
		this._temporalDirection = uniform( 0 );

		/**
		 * Temporal offset added to the initial ray step.
		 *
		 * @private
		 * @type {UniformNode<float>}
		 */
		this._temporalOffset = uniform( 0 );

		/**
		 * Resolution scale uniform.
		 *
		 * @private
		 * @type {UniformNode<float>}
		 */
		this._resolutionScale = uniform( 0 );

		/**
		 * The TSL function that computes the AO. Required for rebuild.
		 *
		 * @private
		 * @type {?Function}
		 * @default null
		 */
		this._ao = null;

		/**
		 * The sample count currently baked into the shader.
		 *
		 * @private
		 * @type {number}
		 */
		this._currentSamples = - 1;

		/**
		 * The shared builder context.  Required for rebuild.
		 *
		 * @private
		 * @type {?Object}
		 * @default null
		 */
		this._sharedContext = null;

		/**
		 * The material that is used to render the effect.
		 *
		 * @private
		 * @type {NodeMaterial}
		 */
		this._material = new NodeMaterial();
		this._material.name = 'GTAO';

		/**
		 * The result of the effect is represented as a separate texture node.
		 *
		 * @private
		 * @type {PassTextureNode}
		 */
		this._textureNode = passTexture( this, this._aoRenderTarget.texture );

	}

	/**
	 * Returns the result of the effect as a texture node.
	 *
	 * @return {PassTextureNode} A texture node that represents the result of the effect.
	 */
	getTextureNode() {

		return this._textureNode;

	}

	/**
	 * Sets the size of the effect.
	 *
	 * @param {number} width - The width of the effect.
	 * @param {number} height - The height of the effect.
	 */
	setSize( width, height ) {

		width = Math.round( this.resolutionScale * width );
		height = Math.round( this.resolutionScale * height );

		this._resolutionScale.value = this.resolutionScale;
		this._resolution.value.set( width, height );
		this._aoRenderTarget.setSize( width, height );

	}

	/**
	 * This method is used to render the effect once per frame.
	 *
	 * @param {NodeFrame} frame - The current node frame.
	 */
	updateBefore( frame ) {

		const { renderer } = frame;

		_rendererState = RendererUtils.resetRendererState( renderer, _rendererState );

		// update temporal uniforms

		if ( this.useTemporalFiltering === true ) {

			const frameId = frame.frameId;

			this._temporalDirection.value = _temporalRotations[ frameId % 6 ] / 360;
			this._temporalOffset.value = _spatialOffsets[ frameId % 4 ];

		} else {

			this._temporalDirection.value = 0;
			this._temporalOffset.value = 1;

		}

		// rebuild the material if the sample count has changed

		if ( this.samples.value !== this._currentSamples ) {

			this._currentSamples = this.samples.value;

			this._material.contextNode = context( this._sharedContext );
			this._material.fragmentNode = this._ao();
			this._material.needsUpdate = true;

		}

		//

		const size = renderer.getDrawingBufferSize( _size );
		this.setSize( size.width, size.height );

		_quadMesh.material = this._material;
		_quadMesh.name = 'AO';

		// clear

		renderer.setClearColor( 0xffffff, 1 );

		// ao

		renderer.setRenderTarget( this._aoRenderTarget );
		_quadMesh.render( renderer );

		// restore

		RendererUtils.restoreRendererState( renderer, _rendererState );

	}

	/**
	 * This method is used to setup the effect's TSL code.
	 *
	 * @param {NodeBuilder} builder - The current node builder.
	 * @return {PassTextureNode}
	 */
	setup( builder ) {

		const uvNode = uv();

		const linearizeDepth = ( depth ) => {

			if ( builder.renderer.logarithmicDepthBuffer === true ) {

				const viewZ = logarithmicDepthToViewZ( depth, this._cameraNear, this._cameraFar );

				return viewZToPerspectiveDepth( viewZ, this._cameraNear, this._cameraFar );

			}

			return depth;

		};

		const sampleDepth = ( uv ) => linearizeDepth( this.depthNode.sample( uv ).r );

		const sampleCenterDepth = ( uv ) => {

			// Sidestep the nearest-rounding during depth access for the unjittered center pixel to avoid banding

			const g = this.depthNode.gather().sample( uv );
			const depth = min( min( g.x, g.y ), min( g.z, g.w ) );

			return linearizeDepth( depth );

		};

		const sampleNoise = ( uv ) => this._noiseNode.sample( uv );
		const sampleNormal = ( uv ) => ( this.normalNode !== null ) ? this.normalNode.sample( uv ).rgb.normalize() : getNormalFromDepth( uv, this.depthNode.value, this._cameraProjectionMatrixInverse );

		// fork: begin. GTAOShader's horizon search and integral (r186 examples/jsm/shaders/GTAOShader.js), which the WebGL GTAOPass drew.
		this._ao = Fn( () => {

			const depth = this._resolutionScale.lessThan( 1 ).select( sampleCenterDepth( uvNode ), sampleDepth( uvNode ) ).toConst();

			depth.greaterThanEqual( 1.0 ).discard();

			const viewPosition = getViewPosition( uvNode, depth, this._cameraProjectionMatrixInverse ).toConst();
			const viewNormal = sampleNormal( uvNode ).toConst();
			const viewDir = this._orthographic.select( vec3( 0, 0, 1 ), normalize( viewPosition.xyz.negate() ) ).toConst();

			const noiseResolution = textureSize( this._noiseNode, 0 );
			const noiseUv = vec2( uvNode.x, uvNode.y.oneMinus() ).mul( this._resolution.div( noiseResolution ) );
			const noiseTexel = sampleNoise( noiseUv ).toConst();
			const randomVec = noiseTexel.xyz.mul( 2.0 ).sub( 1.0 );
			const tangent = vec3( randomVec.xy, 0.0 ).normalize();
			const bitangent = vec3( tangent.y.negate(), tangent.x, 0.0 );
			const kernelMatrix = mat3( tangent, bitangent, vec3( 0.0, 0.0, 1.0 ) );

			const SAMPLES = this.samples.value;
			const DIRECTIONS = SAMPLES < 30 ? 3 : 5;
			const STEPS = Math.floor( ( SAMPLES + DIRECTIONS - 1 ) / DIRECTIONS );

			// Raises one side's horizon to the scene at sampleViewPosition when it is within the thickness.
			const raise = ( cosHorizon, sampleViewPosition, j ) => {

				const sampleUv = getScreenPositionFromClip( this._cameraProjectionMatrix.mul( vec4( sampleViewPosition, 1.0 ) ) ).toConst();
				const viewDelta = getViewPosition( sampleUv, sampleDepth( sampleUv ), this._cameraProjectionMatrixInverse ).sub( viewPosition ).toConst();

				If( abs( viewDelta.z ).lessThan( this.thickness ), () => {

					const sampleCosHorizon = dot( viewDir, normalize( viewDelta ) );
					cosHorizon.addAssign( max( 0, sampleCosHorizon.sub( cosHorizon ).mul( mix( 1, float( 2 ).div( float( j ).add( 2 ) ), this.distanceFallOff ) ) ) );

				} );

			};

			const ao = float( 0 ).toVar();

			Loop( { start: int( 0 ), end: int( DIRECTIONS ), type: 'int', condition: '<' }, ( { i } ) => {

				const angle = float( i ).div( DIRECTIONS ).mul( PI );
				const sampleDir = normalize( kernelMatrix.mul( vec3( cos( angle ), sin( angle ), 0 ) ) ).toConst();
				const sampleScale = noiseTexel.w.mul( 0.5 ).add( 0.5 );

				const sliceBitangent = normalize( cross( sampleDir, viewDir ) ).toConst();
				const sliceTangent = cross( sliceBitangent, viewDir ).toConst();
				const normalInSlice = normalize( viewNormal.sub( sliceBitangent.mul( dot( viewNormal, sliceBitangent ) ) ) ).toConst();

				const tangentToNormalInSlice = cross( normalInSlice, sliceBitangent ).toConst();
				const cosHorizons = vec2( dot( viewDir, tangentToNormalInSlice ), dot( viewDir, tangentToNormalInSlice.negate() ) ).toVar();

				Loop( { end: int( STEPS ), type: 'int', name: 'j', condition: '<' }, ( { j } ) => {

					const offset = sampleDir.mul( this.radius ).mul( sampleScale ).mul( pow( float( j ).add( 1 ).div( STEPS ), this.distanceExponent ) ).toConst();
					raise( cosHorizons.x, viewPosition.add( offset ), j );
					raise( cosHorizons.y, viewPosition.sub( offset ), j );

				} );

				const sinHorizons = cosHorizons.mul( cosHorizons ).oneMinus().sqrt().toConst();
				const nx = dot( normalInSlice, sliceTangent );
				const ny = dot( normalInSlice, viewDir );
				const nxb = acos( cosHorizons.y ).sub( acos( cosHorizons.x ) ).add( sinHorizons.x.mul( cosHorizons.x ) ).sub( sinHorizons.y.mul( cosHorizons.y ) ).mul( 0.5 );
				const nyb = float( 2 ).sub( cosHorizons.x.mul( cosHorizons.x ) ).sub( cosHorizons.y.mul( cosHorizons.y ) ).mul( 0.5 );
				ao.addAssign( nx.mul( nxb ).add( ny.mul( nyb ) ) );

			} );

			ao.assign( clamp( ao.div( DIRECTIONS ), 0, 1 ) );
			ao.assign( pow( ao, this.scale ) );

			return ao;

		} );
		// fork: end

		this._sharedContext = builder.getSharedContext();
		this._currentSamples = this.samples.value;

		this._material.contextNode = context( builder.getSharedContext() );
		this._material.fragmentNode = this._ao();
		this._material.needsUpdate = true;

		//

		return this._textureNode;

	}

	/**
	 * Frees internal resources. This method should be called
	 * when the effect is no longer required.
	 */
	dispose() {

		super.dispose();

		this._aoRenderTarget.dispose();

		this._noiseTexture.dispose();

		this._material.dispose();

	}

}

export default GTAONode;

/**
 * Generates the AO's noise texture for the given size.
 *
 * @param {number} [size=5] - The noise size.
 * @return {DataTexture} The generated noise texture.
 */
function generateMagicSquareNoise( size = 5 ) {

	const noiseSize = Math.floor( size ) % 2 === 0 ? Math.floor( size ) + 1 : Math.floor( size );
	const magicSquare = generateMagicSquare( noiseSize );
	const noiseSquareSize = magicSquare.length;
	const data = new Uint8Array( noiseSquareSize * 4 );

	for ( let inx = 0; inx < noiseSquareSize; ++ inx ) {

		const iAng = magicSquare[ inx ];
		const angle = ( 2 * Math.PI * iAng ) / noiseSquareSize;
		const randomVec = new Vector3(
			Math.cos( angle ),
			Math.sin( angle ),
			0
		).normalize();
		data[ inx * 4 ] = ( randomVec.x * 0.5 + 0.5 ) * 255;
		data[ inx * 4 + 1 ] = ( randomVec.y * 0.5 + 0.5 ) * 255;
		data[ inx * 4 + 2 ] = 127;
		data[ inx * 4 + 3 ] = 255;

	}

	const noiseTexture = new DataTexture( data, noiseSize, noiseSize );
	noiseTexture.wrapS = RepeatWrapping;
	noiseTexture.wrapT = RepeatWrapping;
	noiseTexture.needsUpdate = true;

	return noiseTexture;

}

/**
 * Computes an array of magic square values required to generate the noise texture.
 *
 * @param {number} size - The noise size.
 * @return {Array<number>} The magic square values.
 */
function generateMagicSquare( size ) {

	const noiseSize = Math.floor( size ) % 2 === 0 ? Math.floor( size ) + 1 : Math.floor( size );
	const noiseSquareSize = noiseSize * noiseSize;
	const magicSquare = Array( noiseSquareSize ).fill( 0 );
	let i = Math.floor( noiseSize / 2 );
	let j = noiseSize - 1;

	for ( let num = 1; num <= noiseSquareSize; ) {

		if ( i === - 1 && j === noiseSize ) {

			j = noiseSize - 2;
			i = 0;

		} else {

			if ( j === noiseSize ) {

				j = 0;

			}

			if ( i < 0 ) {

				i = noiseSize - 1;

			}

		}

		if ( magicSquare[ i * noiseSize + j ] !== 0 ) {

			j -= 2;
			i ++;
			continue;

		} else {

			magicSquare[ i * noiseSize + j ] = num ++;

		}

		j ++;
		i --;

	}

	return magicSquare;

}

/**
 * TSL function for creating a Ground Truth Ambient Occlusion (GTAO) effect.
 *
 * @tsl
 * @function
 * @param {Node<float>} depthNode - A node that represents the scene's depth.
 * @param {?Node<vec3>} normalNode - A node that represents the scene's normals.
 * @param {Camera} camera - The camera the scene is rendered with.
 * @returns {GTAONode}
 */
export const ao = ( depthNode, normalNode, camera ) => new GTAONode( nodeObject( depthNode ), nodeObject( normalNode ), camera );
