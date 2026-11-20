// 4D alpha-aware variant of three r186 (0.186.1) examples/jsm/tsl/display/TRAANode.js
//
// Checked against three 0.186.1:
//   - examples/jsm/tsl/display/TRAANode.js: lines 444-607 (TRAANode.setup)
//   - examples/jsm/tsl/utils/TAAUtils.js: lines 17-39 (clipAABB), 52-151 (flickerReduction, sampleCurrentDepth, samplePreviousDepth)
//
// Inherits jitter offsets, history RenderTargets, updateBefore, setSize and dispose
// from upstream TRAANode and overrides only setup(builder) so varianceClipping
// clamps all four channels (rgb + a) against the 3x3 neighborhood AABB and returns
// exact vec4(0) when the neighborhood is empty transparent background.

import { Vector2, FloatType } from 'three/webgpu';
import { Fn, clamp, context, convertToTexture, float, ivec2, max, mix, texture, uv, vec2, velocity, OnBeforeRenderPipeline, OnAfterRenderPipeline } from 'three/tsl';
import TRAANode from 'three/addons/tsl/display/TRAANode.js';
import { flickerReduction, sampleCurrentDepth, samplePreviousDepth } from 'three/addons/tsl/utils/TAAUtils.js';

const _size = /*@__PURE__*/ new Vector2();

/**
 * Four-dimensional variance AABB clip in premultiplied (rgb, a) space.
 * Clamping the scaled displacement strictly to [minColor, maxColor] ensures
 * an empty transparent neighborhood (minColor = maxColor = vec4(0)) returns
 * exact vec4(0) without any 1e-7 alpha residual.
 */
export const clipAABB4D = Fn( ( [ historyColor, minColor, maxColor ] ) => {

	const pClip = maxColor.add( minColor ).mul( 0.5 ).toVar();
	const eClip = maxColor.sub( minColor ).mul( 0.5 ).add( 1e-7 ).toVar();
	const vClip = historyColor.sub( pClip ).toVar();
	const absUnit = vClip.div( eClip ).abs().toVar();
	const maxUnit = max( max( absUnit.x, absUnit.y ), max( absUnit.z, absUnit.w ) ).toVar();
	const clipped = maxUnit.greaterThan( 1.0 ).select(
		pClip.add( vClip.div( maxUnit ) ),
		historyColor
	);
	return clamp( clipped, minColor, maxColor );

} ).setLayout( {
	name: 'clipAABB4D',
	type: 'vec4',
	inputs: [
		{ name: 'historyColor', type: 'vec4' },
		{ name: 'minColor', type: 'vec4' },
		{ name: 'maxColor', type: 'vec4' }
	]
} );

export class TransparentTRAANode extends TRAANode {

	setup( builder ) {

		if ( builder.renderPipeline && ! builder.context.renderPipelineState.viewOffsetOwner ) {

			builder.context.renderPipelineState.viewOffsetOwner = this;

			this._needsPostProcessingSync = true;

			OnBeforeRenderPipeline( () => {

				const size = builder.renderer.getDrawingBufferSize( _size );
				this.setViewOffset( size.width, size.height );

			} );

			OnAfterRenderPipeline( () => {

				this.clearViewOffset();

			} );

		}

		if ( builder.renderer.reversedDepthBuffer === true ) {

			this._historyRenderTarget.depthTexture.type = FloatType;

		}

		if ( builder.context.velocity !== undefined ) {

			this._velocityNode = builder.context.velocity;

		} else {

			this._velocityNode = velocity;

		}

		const varianceClipping = Fn( ( [ positionTexel, currentColor, historyColor, gamma ] ) => {

			const offsets = [
				[ - 1, - 1 ],
				[ - 1, 1 ],
				[ 1, - 1 ],
				[ 1, 1 ],
				[ 1, 0 ],
				[ 0, - 1 ],
				[ 0, 1 ],
				[ - 1, 0 ]
			];

			const moment1 = currentColor.toVar();
			const moment2 = currentColor.pow2().toVar();

			for ( const [ x, y ] of offsets ) {

				const neighbor = this.beautyNode.offset( ivec2( x, y ) ).load( positionTexel ).max( 0 );
				moment1.addAssign( neighbor );
				moment2.addAssign( neighbor.pow2() );

			}

			const N = float( offsets.length + 1 );
			const mean = moment1.div( N );
			const variance = moment2.div( N ).sub( mean.pow2() ).max( 0 ).sqrt().mul( gamma );
			const minColor = mean.sub( variance ).toVar();
			const maxColor = mean.add( variance ).toVar();

			return clipAABB4D( historyColor, minColor, maxColor );

		} );

		const subpixelCorrection = Fn( ( [ velocityUV, textureSize ] ) => {

			const velocityTexel = velocityUV.mul( textureSize );
			const phase = velocityTexel.fract().abs();
			const weight = max( phase, phase.oneMinus() );
			return weight.x.mul( weight.y ).oneMinus().div( 0.75 );

		} ).setLayout( {
			name: 'subpixelCorrection',
			type: 'float',
			inputs: [
				{ name: 'velocityUV', type: 'vec2' },
				{ name: 'textureSize', type: 'ivec2' }
			]
		} );

		const historyNode = texture( this._historyRenderTarget.texture );

		const resolve = Fn( () => {

			const uvNode = uv();
			const textureSize = this.beautyNode.size();
			const positionTexel = uvNode.mul( textureSize );

			const currentDepth = sampleCurrentDepth( this.depthNode, positionTexel, this._cameraNearFar );
			const closestDepth = currentDepth.get( 'closestDepth' );
			const closestPositionTexel = currentDepth.get( 'closestPositionTexel' );
			const farthestDepth = currentDepth.get( 'farthestDepth' );

			const offsetUV = this.velocityNode.load( closestPositionTexel ).xy.mul( vec2( 0.5, - 0.5 ) );

			const historyUV = uvNode.sub( offsetUV );
			const previousDepth = samplePreviousDepth( this._previousDepthNode, historyUV, this._previousCameraProjectionMatrixInverse, this._previousCameraWorldMatrix, this._cameraWorldMatrixInverse, this._cameraNearFar, this.camera );

			const isValidUV = historyUV.greaterThanEqual( 0 ).all().and( historyUV.lessThanEqual( 1 ).all() );
			const isEdge = farthestDepth.sub( closestDepth ).greaterThan( this.edgeDepthDiff );
			const isDisocclusion = closestDepth.sub( previousDepth ).greaterThan( this.depthThreshold );
			const hasValidHistory = isValidUV.and( isEdge.or( isDisocclusion.not() ) );

			const currentColor = this.beautyNode.sample( uvNode );
			const historyColor = historyNode.sample( historyUV );

			const motionFactor = uvNode.sub( historyUV ).mul( textureSize ).length().div( this.maxVelocityLength ).saturate();
			const currentWeight = float( 0.05 ).toVar();

			if ( this.useSubpixelCorrection ) {

				currentWeight.addAssign( subpixelCorrection( offsetUV, textureSize ).mul( 0.25 ) );

			}

			currentWeight.assign( hasValidHistory.select( currentWeight.add( motionFactor ).saturate(), 1 ) );

			const varianceGamma = mix( 0.5, 1, motionFactor.oneMinus().pow2() );
			const clippedHistoryColor = varianceClipping( positionTexel, currentColor, historyColor, varianceGamma );

			return flickerReduction( currentColor, clippedHistoryColor, currentWeight );

		} );

		this._resolveMaterial.fog = false;
		this._resolveMaterial.contextNode = context( builder.getSharedContext() );
		this._resolveMaterial.fragmentNode = resolve();
		this._resolveMaterial.needsUpdate = true;

		return this._textureNode;

	}

}

export default TransparentTRAANode;

export const transparentTRAA = ( beautyNode, depthNode, velocityNode, camera ) =>
	new TransparentTRAANode( convertToTexture( beautyNode ), depthNode, velocityNode, camera );
