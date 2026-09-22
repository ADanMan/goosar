// Факты о контуре на уровне машины, которые десктоп-оболочка передаёт
// в онбординг; views не имеет доступа к Electron IPC.
export interface PerimeterMachineState {
  caBundleMissing: boolean;
}
