import { useEffect } from 'react';
import { useConfigStore, isPerimeterDeliveryProfile } from '@goosar/core/config';

export function UpdateControlReporter() {
  const perimeterProfile = useConfigStore((state) =>
    isPerimeterDeliveryProfile(state.deliveryProfile),
  );

  useEffect(() => {
    window.desktopAPI.setUpdateControl({ perimeterProfile });
  }, [perimeterProfile]);

  return null;
}
