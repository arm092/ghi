package am.ghi.ide;
import com.intellij.lang.parameterInfo.*;
import com.intellij.psi.PsiElement;
import org.jetbrains.annotations.NotNull;
public final class GhiParameterInfo implements ParameterInfoHandler<PsiElement,GhiSymbols.Symbol> {
    @Override public PsiElement findElementForParameterInfo(@NotNull CreateParameterInfoContext context){
        var call=GhiSymbols.forFile(context.getFile()).callAt(context.getFile(),context.getOffset());
        if(call==null)return null;context.setItemsToShow(new Object[]{call.symbol()});return context.getFile().findElementAt(call.open());
    }
    @Override public void showParameterInfo(@NotNull PsiElement element,@NotNull CreateParameterInfoContext context){context.showHint(element,element.getTextOffset(),this);}
    @Override public PsiElement findElementForUpdatingParameterInfo(@NotNull UpdateParameterInfoContext context){
        var call=GhiSymbols.forFile(context.getFile()).callAt(context.getFile(),context.getOffset());
        if(call==null)return null;context.setCurrentParameter(call.parameter());return context.getFile().findElementAt(call.open());
    }
    @Override public void updateParameterInfo(@NotNull PsiElement element,@NotNull UpdateParameterInfoContext context){if(context.getParameterOwner()!=element)context.removeHint();}
    @Override public void updateUI(GhiSymbols.Symbol symbol,@NotNull ParameterInfoUIContext context){
        String text=String.join(", ",symbol.parameters);int start=0,end=0;
        for(int i=0;i<symbol.parameters.size();i++){end=start+symbol.parameters.get(i).length();if(i==context.getCurrentParameterIndex())break;start=end+2;}
        if(symbol.parameters.isEmpty()||context.getCurrentParameterIndex()>=symbol.parameters.size()){start=-1;end=-1;}
        context.setupUIComponentPresentation(text,start,end,false,false,false,context.getDefaultParameterColor());
    }
}
