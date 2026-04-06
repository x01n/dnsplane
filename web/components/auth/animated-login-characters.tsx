'use client'

import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
} from 'react'
import styles from './animated-login-characters.module.css'

export type AnimatedLoginCharactersProps = {
  usernameFocused: boolean
  passwordFocused: boolean
  showPassword: boolean
  username: string
  password: string
  /** 每次登录失败等需播放沮丧动画时递增 */
  errorNonce: number
}

function calcPosition(
  el: HTMLElement | null,
  mouseX: number,
  mouseY: number,
) {
  if (!el) {
    return { faceX: 0, faceY: 0, bodySkew: 0 }
  }
  const rect = el.getBoundingClientRect()
  const cx = rect.left + rect.width / 2
  const cy = rect.top + rect.height / 3
  const dx = mouseX - cx
  const dy = mouseY - cy
  const faceX = Math.max(-15, Math.min(15, dx / 20))
  const faceY = Math.max(-10, Math.min(10, dy / 30))
  const bodySkew = Math.max(-6, Math.min(6, -dx / 120))
  return { faceX, faceY, bodySkew }
}

function calcPupilOffset(
  el: HTMLElement | null,
  maxDist: number,
  mouseX: number,
  mouseY: number,
) {
  if (!el) {
    return { x: 0, y: 0 }
  }
  const rect = el.getBoundingClientRect()
  const cx = rect.left + rect.width / 2
  const cy = rect.top + rect.height / 2
  const dx = mouseX - cx
  const dy = mouseY - cy
  const dist = Math.min(Math.sqrt(dx * dx + dy * dy), maxDist)
  const angle = Math.atan2(dy, dx)
  return { x: Math.cos(angle) * dist, y: Math.sin(angle) * dist }
}

export function AnimatedLoginCharacters({
  usernameFocused,
  passwordFocused,
  showPassword,
  username,
  password,
  errorNonce,
}: AnimatedLoginCharactersProps) {
  const purpleRef = useRef<HTMLDivElement>(null)
  const blackRef = useRef<HTMLDivElement>(null)
  const orangeRef = useRef<HTMLDivElement>(null)
  const yellowRef = useRef<HTMLDivElement>(null)

  const purpleEyeLRef = useRef<HTMLDivElement>(null)
  const blackEyeLRef = useRef<HTMLDivElement>(null)
  const orangePupilLRef = useRef<HTMLDivElement>(null)
  const yellowPupilLRef = useRef<HTMLDivElement>(null)

  const [mouse, setMouse] = useState({ x: 0, y: 0 })
  const [, setLayoutBump] = useState(0)
  const pendingMouse = useRef({ x: 0, y: 0 })
  const frozenMouseRef = useRef({ x: 0, y: 0 })
  const rafMove = useRef(0)

  const [isLookingAtEachOther, setIsLookingAtEachOther] = useState(false)
  const lookTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const [isPurpleBlinking, setIsPurpleBlinking] = useState(false)
  const [isBlackBlinking, setIsBlackBlinking] = useState(false)

  const [isPurplePeeking, setIsPurplePeeking] = useState(false)
  const peekTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  const [loginErrorPose, setLoginErrorPose] = useState(false)
  const [shakeOn, setShakeOn] = useState(false)
  const [orangeMouthVisible, setOrangeMouthVisible] = useState(false)
  const errorAnimTimers = useRef<ReturnType<typeof setTimeout>[]>([])

  const pwdLen = password.length
  const isTyping = usernameFocused
  const isShowingPwd = pwdLen > 0 && showPassword
  const isLookingAway = passwordFocused && !showPassword

  useLayoutEffect(() => {
    setLayoutBump((v) => v + 1)
  }, [])

  useEffect(() => {
    const onMove = (e: MouseEvent) => {
      pendingMouse.current = { x: e.clientX, y: e.clientY }
      if (!rafMove.current) {
        rafMove.current = requestAnimationFrame(() => {
          rafMove.current = 0
          if (!loginErrorPose) {
            setMouse({ ...pendingMouse.current })
          }
        })
      }
    }
    window.addEventListener('mousemove', onMove)
    return () => {
      window.removeEventListener('mousemove', onMove)
      cancelAnimationFrame(rafMove.current)
    }
  }, [loginErrorPose])

  useEffect(() => {
    if (usernameFocused) {
      setIsLookingAtEachOther(true)
      if (lookTimer.current) {
        clearTimeout(lookTimer.current)
      }
      lookTimer.current = setTimeout(() => {
        setIsLookingAtEachOther(false)
      }, 800)
    } else {
      setIsLookingAtEachOther(false)
      if (lookTimer.current) {
        clearTimeout(lookTimer.current)
        lookTimer.current = null
      }
    }
    return () => {
      if (lookTimer.current) {
        clearTimeout(lookTimer.current)
      }
    }
  }, [usernameFocused, username])

  function scheduleBlinkPurple() {
    setTimeout(
      () => {
        setIsPurpleBlinking(true)
        setTimeout(() => {
          setIsPurpleBlinking(false)
          scheduleBlinkPurple()
        }, 150)
      },
      Math.random() * 4000 + 3000,
    )
  }

  function scheduleBlinkBlack() {
    setTimeout(
      () => {
        setIsBlackBlinking(true)
        setTimeout(() => {
          setIsBlackBlinking(false)
          scheduleBlinkBlack()
        }, 150)
      },
      Math.random() * 4000 + 3000,
    )
  }

  useEffect(() => {
    scheduleBlinkPurple()
    scheduleBlinkBlack()
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 仅挂载时启动随机眨眼循环
  }, [])

  function schedulePeek() {
    if (peekTimer.current) {
      clearTimeout(peekTimer.current)
      peekTimer.current = null
    }
    if (password.length === 0 || !showPassword) {
      return
    }
    peekTimer.current = setTimeout(() => {
      peekTimer.current = null
      if (password.length > 0 && showPassword) {
        setIsPurplePeeking(true)
        setTimeout(() => {
          setIsPurplePeeking(false)
          schedulePeek()
        }, 800)
      }
    }, Math.random() * 3000 + 2000)
  }

  useEffect(() => {
    if (showPassword && password.length > 0) {
      schedulePeek()
    }
    return () => {
      if (peekTimer.current) {
        clearTimeout(peekTimer.current)
        peekTimer.current = null
      }
    }
    // schedulePeek 为组件内递归函数，随 password/showPassword 语义变化；依赖已列出
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [showPassword, password.length])

  useLayoutEffect(() => {
    if (errorNonce === 0) {
      return
    }
    errorAnimTimers.current.forEach(clearTimeout)
    errorAnimTimers.current = []

    frozenMouseRef.current = { ...pendingMouse.current }

    setShakeOn(false)
    setLoginErrorPose(false)
    setOrangeMouthVisible(false)

    const t0 = requestAnimationFrame(() => {
      void document.body.offsetHeight
      setLoginErrorPose(true)
      setOrangeMouthVisible(true)
      const tShake = setTimeout(() => setShakeOn(true), 350)
      const tEnd = setTimeout(() => {
        setLoginErrorPose(false)
        setOrangeMouthVisible(false)
        setShakeOn(false)
      }, 2500)
      errorAnimTimers.current.push(tShake, tEnd)
    })
    return () => {
      cancelAnimationFrame(t0)
    }
  }, [errorNonce])

  useEffect(() => {
    if (!loginErrorPose) {
      setMouse({ ...pendingMouse.current })
    }
  }, [loginErrorPose])

  const mouseX = loginErrorPose ? frozenMouseRef.current.x : mouse.x
  const mouseY = loginErrorPose ? frozenMouseRef.current.y : mouse.y

  const purple = purpleRef.current
  const black = blackRef.current
  const orange = orangeRef.current
  const yellow = yellowRef.current

  const purplePos = calcPosition(purple, mouseX, mouseY)
  const blackPos = calcPosition(black, mouseX, mouseY)
  const orangePos = calcPosition(orange, mouseX, mouseY)
  const yellowPos = calcPosition(yellow, mouseX, mouseY)

  let purpleStyle: CSSProperties = {}
  if (isShowingPwd) {
    purpleStyle = { transform: 'skewX(0deg)', height: 370 }
  } else if (loginErrorPose) {
    purpleStyle = { transform: 'skewX(0deg)', height: 370 }
  } else if (isLookingAway) {
    purpleStyle = {
      transform: 'skewX(-14deg) translateX(-20px)',
      height: 410,
    }
  } else if (isTyping) {
    purpleStyle = {
      transform: `skewX(${(purplePos.bodySkew || 0) - 12}deg) translateX(40px)`,
      height: 410,
    }
  } else {
    purpleStyle = {
      transform: `skewX(${purplePos.bodySkew}deg)`,
      height: 370,
    }
  }

  let purpleEyesStyle: CSSProperties = {}
  let purplePupilL = ''
  let purplePupilR = ''
  const purpleEyeH = isPurpleBlinking ? 2 : 18

  if (loginErrorPose) {
    purpleEyesStyle = { left: 30, top: 55 }
    purplePupilL = 'translate(-3px, 4px)'
    purplePupilR = 'translate(-3px, 4px)'
  } else if (isLookingAway) {
    purpleEyesStyle = { left: 20, top: 25 }
    purplePupilL = 'translate(-5px, -5px)'
    purplePupilR = 'translate(-5px, -5px)'
  } else if (isShowingPwd) {
    purpleEyesStyle = { left: 20, top: 35 }
    const px = isPurplePeeking ? 4 : -4
    const py = isPurplePeeking ? 5 : -4
    purplePupilL = `translate(${px}px, ${py}px)`
    purplePupilR = `translate(${px}px, ${py}px)`
  } else if (isLookingAtEachOther) {
    purpleEyesStyle = { left: 55, top: 65 }
    purplePupilL = 'translate(3px, 4px)'
    purplePupilR = 'translate(3px, 4px)'
  } else {
    purpleEyesStyle = {
      left: 45 + purplePos.faceX,
      top: 40 + purplePos.faceY,
    }
    const po = calcPupilOffset(
      purpleEyeLRef.current,
      5,
      mouseX,
      mouseY,
    )
    purplePupilL = `translate(${po.x}px, ${po.y}px)`
    purplePupilR = `translate(${po.x}px, ${po.y}px)`
  }

  let blackStyle: CSSProperties = {}
  if (isShowingPwd) {
    blackStyle = { transform: 'skewX(0deg)' }
  } else if (loginErrorPose) {
    blackStyle = { transform: 'skewX(0deg)' }
  } else if (isLookingAway) {
    blackStyle = { transform: 'skewX(12deg) translateX(-10px)' }
  } else if (isLookingAtEachOther) {
    blackStyle = {
      transform: `skewX(${(blackPos.bodySkew || 0) * 1.5 + 10}deg) translateX(20px)`,
    }
  } else if (isTyping) {
    blackStyle = { transform: `skewX(${(blackPos.bodySkew || 0) * 1.5}deg)` }
  } else {
    blackStyle = { transform: `skewX(${blackPos.bodySkew}deg)` }
  }

  let blackEyesStyle: CSSProperties = {}
  let blackPupilL = ''
  let blackPupilR = ''
  const blackEyeH = isBlackBlinking ? 2 : 16

  if (loginErrorPose) {
    blackEyesStyle = { left: 15, top: 40 }
    blackPupilL = 'translate(-3px, 4px)'
    blackPupilR = 'translate(-3px, 4px)'
  } else if (isLookingAway) {
    blackEyesStyle = { left: 10, top: 20 }
    blackPupilL = 'translate(-4px, -5px)'
    blackPupilR = 'translate(-4px, -5px)'
  } else if (isShowingPwd) {
    blackEyesStyle = { left: 10, top: 28 }
    blackPupilL = 'translate(-4px, -4px)'
    blackPupilR = 'translate(-4px, -4px)'
  } else if (isLookingAtEachOther) {
    blackEyesStyle = { left: 32, top: 12 }
    blackPupilL = 'translate(0px, -4px)'
    blackPupilR = 'translate(0px, -4px)'
  } else {
    blackEyesStyle = {
      left: 26 + blackPos.faceX,
      top: 32 + blackPos.faceY,
    }
    const bo = calcPupilOffset(blackEyeLRef.current, 4, mouseX, mouseY)
    blackPupilL = `translate(${bo.x}px, ${bo.y}px)`
    blackPupilR = `translate(${bo.x}px, ${bo.y}px)`
  }

  let orangeStyle: CSSProperties = {}
  if (isShowingPwd || loginErrorPose) {
    orangeStyle = { transform: 'skewX(0deg)' }
  } else {
    orangeStyle = { transform: `skewX(${orangePos.bodySkew}deg)` }
  }

  let orangeEyesStyle: CSSProperties = {}
  let orangePupilL = ''
  let orangePupilR = ''
  let orangeMouthPos: CSSProperties = {}

  if (loginErrorPose) {
    orangeEyesStyle = { left: 60, top: 95 }
    orangePupilL = 'translate(-3px, 4px)'
    orangePupilR = 'translate(-3px, 4px)'
    orangeMouthPos = { left: 80 + orangePos.faceX, top: 130 }
  } else if (isLookingAway) {
    orangeEyesStyle = { left: 50, top: 75 }
    orangePupilL = 'translate(-5px, -5px)'
    orangePupilR = 'translate(-5px, -5px)'
  } else if (isShowingPwd) {
    orangeEyesStyle = { left: 50, top: 85 }
    orangePupilL = 'translate(-5px, -4px)'
    orangePupilR = 'translate(-5px, -4px)'
  } else {
    orangeEyesStyle = {
      left: 82 + orangePos.faceX,
      top: 90 + orangePos.faceY,
    }
    const oo = calcPupilOffset(orangePupilLRef.current, 5, mouseX, mouseY)
    orangePupilL = `translate(${oo.x}px, ${oo.y}px)`
    orangePupilR = `translate(${oo.x}px, ${oo.y}px)`
  }

  let yellowStyle: CSSProperties = {}
  if (isShowingPwd || loginErrorPose) {
    yellowStyle = { transform: 'skewX(0deg)' }
  } else {
    yellowStyle = { transform: `skewX(${yellowPos.bodySkew}deg)` }
  }

  let yellowEyesStyle: CSSProperties = {}
  let yellowPupilL = ''
  let yellowPupilR = ''
  let yellowMouthStyle: CSSProperties = {}

  if (loginErrorPose) {
    yellowEyesStyle = { left: 35, top: 45 }
    yellowPupilL = 'translate(-3px, 4px)'
    yellowPupilR = 'translate(-3px, 4px)'
    yellowMouthStyle = { left: 30, top: 92, transform: 'rotate(-8deg)' }
  } else if (isLookingAway) {
    yellowEyesStyle = { left: 20, top: 30 }
    yellowPupilL = 'translate(-5px, -5px)'
    yellowPupilR = 'translate(-5px, -5px)'
    yellowMouthStyle = { left: 15, top: 78, transform: 'rotate(0deg)' }
  } else if (isShowingPwd) {
    yellowEyesStyle = { left: 20, top: 35 }
    yellowPupilL = 'translate(-5px, -4px)'
    yellowPupilR = 'translate(-5px, -4px)'
    yellowMouthStyle = { left: 10, top: 88, transform: 'rotate(0deg)' }
  } else {
    yellowEyesStyle = {
      left: 52 + yellowPos.faceX,
      top: 40 + yellowPos.faceY,
    }
    const yo = calcPupilOffset(yellowPupilLRef.current, 5, mouseX, mouseY)
    yellowPupilL = `translate(${yo.x}px, ${yo.y}px)`
    yellowPupilR = `translate(${yo.x}px, ${yo.y}px)`
    yellowMouthStyle = {
      left: 40 + yellowPos.faceX,
      top: 88 + yellowPos.faceY,
      transform: 'rotate(0deg)',
    }
  }

  const shakeClass = shakeOn ? styles.shakeHead : ''

  return (
    <div className={styles.charactersWrapper}>
      <div className={styles.charactersScene}>
        <div
          ref={purpleRef}
          className={`${styles.character} ${styles.charPurple}`}
          style={purpleStyle}
        >
          <div
            className={`${styles.eyes} ${shakeClass}`}
            style={{ ...purpleEyesStyle, gap: 28 }}
          >
            <div
              ref={purpleEyeLRef}
              className={styles.eyeball}
              style={{ width: 18, height: purpleEyeH }}
            >
              <div
                className={styles.pupil}
                style={{ width: 7, height: 7, transform: purplePupilL }}
              />
            </div>
            <div
              className={styles.eyeball}
              style={{ width: 18, height: purpleEyeH }}
            >
              <div
                className={styles.pupil}
                style={{ width: 7, height: 7, transform: purplePupilR }}
              />
            </div>
          </div>
        </div>

        <div
          ref={blackRef}
          className={`${styles.character} ${styles.charBlack}`}
          style={blackStyle}
        >
          <div
            className={`${styles.eyes} ${shakeClass}`}
            style={{ ...blackEyesStyle, gap: 20 }}
          >
            <div
              ref={blackEyeLRef}
              className={styles.eyeball}
              style={{ width: 16, height: blackEyeH }}
            >
              <div
                className={styles.pupil}
                style={{ width: 6, height: 6, transform: blackPupilL }}
              />
            </div>
            <div
              className={styles.eyeball}
              style={{ width: 16, height: blackEyeH }}
            >
              <div
                className={styles.pupil}
                style={{ width: 6, height: 6, transform: blackPupilR }}
              />
            </div>
          </div>
        </div>

        <div
          ref={orangeRef}
          className={`${styles.character} ${styles.charOrange}`}
          style={orangeStyle}
        >
          <div
            className={`${styles.eyes} ${shakeClass}`}
            style={{ ...orangeEyesStyle, gap: 28 }}
          >
            <div
              ref={orangePupilLRef}
              className={styles.barePupil}
              style={{ transform: orangePupilL }}
            />
            <div
              className={styles.barePupil}
              style={{ transform: orangePupilR }}
            />
          </div>
          <div
            className={`${styles.orangeMouth} ${orangeMouthVisible ? styles.orangeMouthVisible : ''} ${shakeClass}`}
            style={orangeMouthPos}
          />
        </div>

        <div
          ref={yellowRef}
          className={`${styles.character} ${styles.charYellow}`}
          style={yellowStyle}
        >
          <div
            className={`${styles.eyes} ${shakeClass}`}
            style={{ ...yellowEyesStyle, gap: 20 }}
          >
            <div
              ref={yellowPupilLRef}
              className={styles.barePupil}
              style={{ transform: yellowPupilL }}
            />
            <div
              className={styles.barePupil}
              style={{ transform: yellowPupilR }}
            />
          </div>
          <div
            className={`${styles.yellowMouth} ${shakeClass}`}
            style={yellowMouthStyle}
          />
        </div>
      </div>
    </div>
  )
}
