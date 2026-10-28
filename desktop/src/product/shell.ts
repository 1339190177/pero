import { mountAppSurfaces } from '../../../web/src/apps';
import type { HostEvent } from '../../../web/src/bootstrap';
import type { ProductFields } from '../../../packages/product/product';
type Context = { product: ProductFields; locale: string; dark: boolean };
const bridge = (window as unknown as { product: {
  connect(): Promise<Context>; open(): Promise<void>;
  onEvent(receive: (value: HostEvent) => void): void;
  onContext(receive: (value: Context) => void): void;
} }).product;
const surface = mountAppSurfaces(async () => bridge.open());
let context: Context;
function update() {
  if (!context) return;
  document.title = context.product.name;
  surface.surface({ visible: true, ...context.product.entry, name: context.product.entry!.app,
    dark: context.dark, locale: context.locale, width: innerWidth, height: innerHeight });
}
bridge.onEvent(event => surface.event(event));
bridge.onContext(value => { context = value; update(); });
window.addEventListener('resize', update);
bridge.connect().then(value => { context = value; update(); return bridge.open(); }).catch(error => {
  surface.event({ kind: 'apps_error', text: error.message } as HostEvent);
});
