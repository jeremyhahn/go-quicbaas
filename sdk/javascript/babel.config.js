/**
 * Babel configuration for QuicBaaS JavaScript SDK
 */

export default {
  presets: [
    ['@babel/preset-env', {
      targets: {
        node: 'current',
      },
    }],
  ],
};
